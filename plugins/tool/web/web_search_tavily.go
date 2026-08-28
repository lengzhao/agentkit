package web

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/credentials"
)

const (
	defaultTavilyBaseURL    = "https://api.tavily.com"
	defaultTavilyMaxResults = 5
)

type WebSearchTavilyConfig struct {
	// APIKeyRef is credential ref resolved via deps.credentials, e.g. "env:TAVILY_API_KEY".
	APIKeyRef string `json:"apiKeyRef,omitempty"`
	// APIKey is literal key; prefer APIKeyRef so the secret stays out of config files.
	APIKey string `json:"apiKey,omitempty"`
	// BaseURL overrides the API host; defaults to https://api.tavily.com.
	BaseURL string `json:"baseUrl,omitempty"`
	// MaxResults is cap on hits per call; defaults to 5.
	MaxResults int `json:"maxResults,omitempty"`
	// TimeoutSeconds is per-request wall clock; defaults to 30.
	TimeoutSeconds int `json:"timeoutSeconds,omitempty"`
	// SearchDepth is Tavily search mode: basic (default) or advanced.
	SearchDepth string `json:"searchDepth,omitempty"`
	// Topic narrows results, e.g. "general" or "news".
	Topic string `json:"topic,omitempty"`
	// IncludeAnswer also requests Tavily's synthesized answer; costs more tokens.
	IncludeAnswer bool `json:"includeAnswer,omitempty"`
	// SnippetChars is snippet truncation limit; defaults to 800.
	SnippetChars int `json:"snippetChars,omitempty"`
	// IncludeDomains restricts results to these domains.
	IncludeDomains []string `json:"includeDomains,omitempty"`
	// ExcludeDomains drops results from these domains.
	ExcludeDomains []string `json:"excludeDomains,omitempty"`
}

type WebSearchTavilyDeps struct {
	Credentials credentials.Store `json:"credentials,omitempty"`
}

type tavilySearcher struct {
	client         *http.Client
	apiKey         string
	baseURL        string
	maxResults     int
	searchDepth    string
	topic          string
	includeAnswer  bool
	snippetChars   int
	includeDomains []string
	excludeDomains []string
}

type tavilyRequest struct {
	Query          string   `json:"query"`
	MaxResults     int      `json:"max_results,omitempty"`
	SearchDepth    string   `json:"search_depth,omitempty"`
	Topic          string   `json:"topic,omitempty"`
	IncludeAnswer  bool     `json:"include_answer,omitempty"`
	IncludeDomains []string `json:"include_domains,omitempty"`
	ExcludeDomains []string `json:"exclude_domains,omitempty"`
}

type tavilyResponse struct {
	Query   string `json:"query"`
	Answer  string `json:"answer,omitempty"`
	Results []struct {
		Title   string  `json:"title"`
		URL     string  `json:"url"`
		Content string  `json:"content"`
		Score   float64 `json:"score"`
	} `json:"results"`
}

// NewWebSearchTavily registers tool/web-search-tavily: Search the web through Tavily (tool name: web_search).
//
// Best practices:
//   - A missing key is reported at call time, not at build time.
//   - Search returns snippets; pair with tool/web-fetch-http when the model needs the full page.
func NewWebSearchTavily(cfg WebSearchTavilyConfig, deps WebSearchTavilyDeps) (agentkit.ToolPack, error) {
	t := newTavilySearcher(cfg, deps)

	tool, err := agentkit.NewTool[WebSearchInput, WebSearchOutput]("web_search", func(ctx context.Context, input WebSearchInput) (WebSearchOutput, error) {
		query := strings.TrimSpace(input.Query)
		if query == "" {
			return WebSearchOutput{}, fmt.Errorf("query is required")
		}
		max := input.MaxResults
		if max <= 0 {
			max = t.maxResults
		}
		result, err := t.search(ctx, query, max)
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

func newTavilySearcher(cfg WebSearchTavilyConfig, deps WebSearchTavilyDeps) *tavilySearcher {
	timeout := 30 * time.Second
	if cfg.TimeoutSeconds > 0 {
		timeout = time.Duration(cfg.TimeoutSeconds) * time.Second
	}
	maxResults := cfg.MaxResults
	if maxResults <= 0 {
		maxResults = defaultTavilyMaxResults
	}
	snippetChars := cfg.SnippetChars
	if snippetChars <= 0 {
		snippetChars = defaultSnippetChars
	}
	baseURL := strings.TrimRight(cfg.BaseURL, "/")
	if baseURL == "" {
		baseURL = defaultTavilyBaseURL
	}
	return &tavilySearcher{
		client:         &http.Client{Timeout: timeout},
		apiKey:         resolveSearchKey(cfg.APIKey, cfg.APIKeyRef, "TAVILY_API_KEY", deps.Credentials),
		baseURL:        baseURL,
		maxResults:     maxResults,
		searchDepth:    cfg.SearchDepth,
		topic:          cfg.Topic,
		includeAnswer:  cfg.IncludeAnswer,
		snippetChars:   snippetChars,
		includeDomains: cfg.IncludeDomains,
		excludeDomains: cfg.ExcludeDomains,
	}
}

func (t *tavilySearcher) search(ctx context.Context, query string, maxResults int) (WebSearchOutput, error) {
	if t.apiKey == "" {
		return WebSearchOutput{}, fmt.Errorf("tool/web-search-tavily has no API key: set TAVILY_API_KEY, or config.apiKeyRef with a credentials dep")
	}
	n := maxResults
	if n <= 0 || n > t.maxResults {
		n = t.maxResults
	}

	payload := tavilyRequest{
		Query:          query,
		MaxResults:     n,
		SearchDepth:    t.searchDepth,
		Topic:          t.topic,
		IncludeAnswer:  t.includeAnswer,
		IncludeDomains: t.includeDomains,
		ExcludeDomains: t.excludeDomains,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return WebSearchOutput{}, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, t.baseURL+"/search", bytes.NewReader(body))
	if err != nil {
		return WebSearchOutput{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+t.apiKey)

	resp, err := t.client.Do(httpReq)
	if err != nil {
		return WebSearchOutput{}, fmt.Errorf("tavily search: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return WebSearchOutput{}, fmt.Errorf("tavily search: read response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return WebSearchOutput{}, fmt.Errorf("tavily search: http %d: %s", resp.StatusCode, truncateText(strings.TrimSpace(string(raw)), 300))
	}

	var decoded tavilyResponse
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return WebSearchOutput{}, fmt.Errorf("tavily search: decode response: %w", err)
	}

	out := WebSearchOutput{Query: query, Provider: "tavily"}
	for _, r := range decoded.Results {
		out.Results = append(out.Results, WebSearchHit{
			Title:   strings.TrimSpace(r.Title),
			URL:     r.URL,
			Snippet: truncateText(collapse(strings.TrimSpace(r.Content)), t.snippetChars),
		})
	}
	return out, nil
}
