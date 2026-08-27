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

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/credentials"
)

const (
	defaultExaBaseURL    = "https://api.exa.ai"
	defaultExaMaxResults = 5
	defaultSnippetChars  = 800
)

type WebSearchExaConfig struct {
	// APIKeyRef is credential ref resolved via deps.credentials, e.g. "env:EXA_API_KEY".
	APIKeyRef string `json:"apiKeyRef,omitempty"`
	// APIKey is literal key; prefer APIKeyRef so the secret stays out of config files.
	APIKey string `json:"apiKey,omitempty"`
	// BaseURL overrides the API host; defaults to https://api.exa.ai.
	BaseURL string `json:"baseUrl,omitempty"`
	// MaxResults is cap on hits per call; defaults to 5.
	MaxResults int `json:"maxResults,omitempty"`
	// TimeoutSeconds is per-request wall clock; defaults to 30.
	TimeoutSeconds int `json:"timeoutSeconds,omitempty"`
	// Type is Exa search mode: auto (default), fast, instant, deep.
	Type string `json:"type,omitempty"`
	// Category narrows results, e.g. "news" or "research paper".
	Category string `json:"category,omitempty"`
	// IncludeText also requests page text; costs more tokens and money than highlights alone.
	IncludeText bool `json:"includeText,omitempty"`
	// SnippetChars is snippet truncation limit; defaults to 800.
	SnippetChars int `json:"snippetChars,omitempty"`
	// IncludeDomains restricts results to these domains.
	IncludeDomains []string `json:"includeDomains,omitempty"`
	// ExcludeDomains drops results from these domains.
	ExcludeDomains []string `json:"excludeDomains,omitempty"`
}

type WebSearchExaDeps struct {
	Credentials credentials.Store `json:"credentials,omitempty"`
}

type WebSearchHit struct {
	Title       string `json:"title"`
	URL         string `json:"url"`
	Snippet     string `json:"snippet,omitempty"`
	PublishedAt string `json:"publishedAt,omitempty"`
}

type WebSearchInput struct {
	Query      string `json:"query" jsonschema:"What to search for, as a natural-language query"`
	MaxResults int    `json:"maxResults,omitempty" jsonschema:"Maximum hits to return"`
}

type WebSearchOutput struct {
	Query    string         `json:"query"`
	Provider string         `json:"provider,omitempty"`
	Results  []WebSearchHit `json:"results"`
}

type exaSearcher struct {
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

// NewWebSearchExa registers tool/web-search-exa: Search the web through Exa (tool name: web_search).
//
// Best practices:
//   - A missing key is reported at call time, not at build time.
//   - Search returns snippets; pair with tool/web-fetch-http when the model needs the full page.
func NewWebSearchExa(cfg WebSearchExaConfig, deps WebSearchExaDeps) (agentkit.ToolPack, error) {
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
	e := &exaSearcher{
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
	}

	tool, err := agentkit.NewTool[WebSearchInput, WebSearchOutput]("web_search", func(ctx context.Context, input WebSearchInput) (WebSearchOutput, error) {
		query := strings.TrimSpace(input.Query)
		if query == "" {
			return WebSearchOutput{}, fmt.Errorf("query is required")
		}
		max := input.MaxResults
		if max <= 0 {
			max = e.maxResults
		}
		result, err := e.search(ctx, query, max)
		if err != nil {
			return WebSearchOutput{}, err
		}
		return result, nil
	}).Description("Search the web and return ranked results with snippets. Snippets are excerpts, not the whole page: fetch a result's url when you need its details.").Build()
	if err != nil {
		return nil, err
	}
	return agentkit.Pack(tool), nil
}

func (e *exaSearcher) search(ctx context.Context, query string, maxResults int) (WebSearchOutput, error) {
	if e.apiKey == "" {
		return WebSearchOutput{}, fmt.Errorf("tool/web-search-exa has no API key: set EXA_API_KEY, or config.apiKeyRef with a credentials dep")
	}
	n := maxResults
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
		return WebSearchOutput{}, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, e.baseURL+"/search", bytes.NewReader(body))
	if err != nil {
		return WebSearchOutput{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", e.apiKey)

	resp, err := e.client.Do(httpReq)
	if err != nil {
		return WebSearchOutput{}, fmt.Errorf("exa search: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return WebSearchOutput{}, fmt.Errorf("exa search: read response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return WebSearchOutput{}, fmt.Errorf("exa search: http %d: %s", resp.StatusCode, truncateText(strings.TrimSpace(string(raw)), 300))
	}

	var decoded exaResponse
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return WebSearchOutput{}, fmt.Errorf("exa search: decode response: %w", err)
	}

	out := WebSearchOutput{Query: query, Provider: "exa"}
	for _, r := range decoded.Results {
		snippet := strings.TrimSpace(strings.Join(r.Highlights, " … "))
		if snippet == "" {
			snippet = strings.TrimSpace(r.Text)
		}
		out.Results = append(out.Results, WebSearchHit{
			Title:       strings.TrimSpace(r.Title),
			URL:         r.URL,
			Snippet:     truncateText(collapse(snippet), e.snippetChars),
			PublishedAt: r.PublishedDate,
		})
	}
	return out, nil
}

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

func truncateText(s string, max int) string {
	if max <= 0 || len(s) <= max {
		return s
	}
	cut := max
	for cut > 0 && s[cut]&0xc0 == 0x80 {
		cut--
	}
	return s[:cut] + "…"
}
