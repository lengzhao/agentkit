package web

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/lengzhao/agentkit/cap/credentials"
	"github.com/lengzhao/agentkit/cap/web"
	"github.com/lengzhao/pluginkit"
)

const (
	defaultExaBaseURL    = "https://api.exa.ai"
	defaultExaMaxResults = 5
	defaultSnippetChars  = 800
)

type ExaConfig struct {
	// APIKeyRef is resolved through deps.credentials, e.g. "env:EXA_API_KEY".
	// When it cannot be resolved the provider still builds and reports the
	// missing key at call time, so mounting search alongside web/http-fetch
	// never breaks a keyless setup.
	APIKeyRef      string `json:"apiKeyRef,omitempty"`
	APIKey         string `json:"apiKey,omitempty"`
	BaseURL        string `json:"baseUrl,omitempty"`
	MaxResults     int    `json:"maxResults,omitempty"`
	TimeoutSeconds int    `json:"timeoutSeconds,omitempty"`
	// Type selects Exa's search mode: auto (default), fast, instant, deep...
	Type string `json:"type,omitempty"`
	// Category narrows results, e.g. "research paper", "news", "company".
	Category string `json:"category,omitempty"`
	// IncludeText additionally requests page text, which costs more tokens and
	// more money than the query-relevant highlights alone.
	IncludeText    bool     `json:"includeText,omitempty"`
	SnippetChars   int      `json:"snippetChars,omitempty"`
	IncludeDomains []string `json:"includeDomains,omitempty"`
	ExcludeDomains []string `json:"excludeDomains,omitempty"`
}

type ExaDeps struct {
	Credentials credentials.Store `json:"credentials,omitempty"`
}

type Exa struct {
	client       *http.Client
	apiKey       string
	baseURL      string
	maxResults   int
	searchType   string
	category     string
	includeText  bool
	snippetChars int
	include      []string
	exclude      []string
}

func init() {
	pluginkit.Register("web/exa-search", NewExa)
}

func NewExa(cfg ExaConfig, deps ExaDeps) (web.Searcher, error) {
	timeout := 30 * time.Second
	if cfg.TimeoutSeconds > 0 {
		timeout = time.Duration(cfg.TimeoutSeconds) * time.Second
	}
	maxResults := cfg.MaxResults
	if maxResults <= 0 {
		maxResults = defaultExaMaxResults
	}
	snippetChars := cfg.SnippetChars
	if snippetChars <= 0 {
		snippetChars = defaultSnippetChars
	}
	baseURL := strings.TrimRight(cfg.BaseURL, "/")
	if baseURL == "" {
		baseURL = defaultExaBaseURL
	}
	return &Exa{
		client:       &http.Client{Timeout: timeout},
		apiKey:       resolveSearchKey(cfg.APIKey, cfg.APIKeyRef, "EXA_API_KEY", deps.Credentials),
		baseURL:      baseURL,
		maxResults:   maxResults,
		searchType:   cfg.Type,
		category:     cfg.Category,
		includeText:  cfg.IncludeText,
		snippetChars: snippetChars,
		include:      cfg.IncludeDomains,
		exclude:      cfg.ExcludeDomains,
	}, nil
}

// exaContents mirrors Exa's camelCase request shape; the Python SDK's snake_case
// is an SDK convention, not the wire format.
type exaContents struct {
	Highlights bool           `json:"highlights,omitempty"`
	Text       *exaTextConfig `json:"text,omitempty"`
}

type exaTextConfig struct {
	MaxCharacters int `json:"maxCharacters,omitempty"`
}

type exaRequest struct {
	Query          string      `json:"query"`
	NumResults     int         `json:"numResults,omitempty"`
	Type           string      `json:"type,omitempty"`
	Category       string      `json:"category,omitempty"`
	IncludeDomains []string    `json:"includeDomains,omitempty"`
	ExcludeDomains []string    `json:"excludeDomains,omitempty"`
	Contents       exaContents `json:"contents"`
}

type exaResponse struct {
	Results []struct {
		Title         string   `json:"title"`
		URL           string   `json:"url"`
		PublishedDate string   `json:"publishedDate"`
		Text          string   `json:"text"`
		Highlights    []string `json:"highlights"`
	} `json:"results"`
}

func (e *Exa) Search(ctx context.Context, req web.SearchRequest) (web.SearchResult, error) {
	query := strings.TrimSpace(req.Query)
	if query == "" {
		return web.SearchResult{}, fmt.Errorf("query is required")
	}
	if e.apiKey == "" {
		return web.SearchResult{}, fmt.Errorf("web/exa-search has no API key: set EXA_API_KEY, or config.apiKeyRef with a credentials dep")
	}
	n := req.MaxResults
	if n <= 0 || n > e.maxResults {
		n = e.maxResults
	}

	payload := exaRequest{
		Query:          query,
		NumResults:     n,
		Type:           e.searchType,
		Category:       e.category,
		IncludeDomains: e.include,
		ExcludeDomains: e.exclude,
		Contents:       exaContents{Highlights: true},
	}
	if e.includeText {
		payload.Contents.Text = &exaTextConfig{MaxCharacters: e.snippetChars}
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return web.SearchResult{}, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, e.baseURL+"/search", bytes.NewReader(body))
	if err != nil {
		return web.SearchResult{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", e.apiKey)

	resp, err := e.client.Do(httpReq)
	if err != nil {
		return web.SearchResult{}, fmt.Errorf("exa search: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return web.SearchResult{}, fmt.Errorf("exa search: read response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return web.SearchResult{}, fmt.Errorf("exa search: http %d: %s", resp.StatusCode, truncate(strings.TrimSpace(string(raw)), 300))
	}

	var decoded exaResponse
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return web.SearchResult{}, fmt.Errorf("exa search: decode response: %w", err)
	}

	out := web.SearchResult{Query: query, Provider: "exa"}
	for _, r := range decoded.Results {
		snippet := strings.TrimSpace(strings.Join(r.Highlights, " … "))
		if snippet == "" {
			snippet = strings.TrimSpace(r.Text)
		}
		out.Results = append(out.Results, web.SearchHit{
			Title:       strings.TrimSpace(r.Title),
			URL:         r.URL,
			Snippet:     truncate(collapse(snippet), e.snippetChars),
			PublishedAt: r.PublishedDate,
		})
	}
	return out, nil
}

// resolveSearchKey mirrors runtime/llm's precedence (literal, then ref, then
// env) but never fails the build: a search provider without a key is a tool
// that reports a missing key, not a broken instance graph.
func resolveSearchKey(apiKey, apiKeyRef, envVar string, store credentials.Store) string {
	if apiKey != "" {
		return apiKey
	}
	if apiKeyRef != "" {
		if store == nil {
			slog.Warn("web search apiKeyRef set without credentials dep", "ref", apiKeyRef)
		} else if secret, err := store.Resolve(context.Background(), apiKeyRef); err != nil {
			slog.Warn("web search apiKeyRef unresolved", "ref", apiKeyRef, "error", err)
		} else if secret.Value != "" {
			return secret.Value
		}
	}
	return os.Getenv(envVar)
}

func truncate(s string, max int) string {
	if max <= 0 || len(s) <= max {
		return s
	}
	// Cut on a rune boundary so the result stays valid UTF-8.
	cut := max
	for cut > 0 && s[cut]&0xc0 == 0x80 {
		cut--
	}
	return s[:cut] + "…"
}
