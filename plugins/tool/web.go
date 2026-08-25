package tool

import (
	"context"
	"fmt"
	"strings"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/web"
)

type WebFetchConfig struct {
	// MaxBytes is per-call read limit on top of the provider's own; 0 uses the provider default.
	MaxBytes int `json:"maxBytes,omitempty"`
}

type WebFetchDeps struct {
	Web web.Fetcher `json:"web"`
}

type WebFetchInput struct {
	URL string `json:"url" jsonschema:"required,description=Absolute http or https URL to fetch"`
	Raw bool   `json:"raw,omitempty" jsonschema:"description=Return the body as served instead of extracting readable text from HTML"`
}

type WebFetchOutput struct {
	URL         string `json:"url"`
	Status      int    `json:"status"`
	ContentType string `json:"contentType,omitempty"`
	Title       string `json:"title,omitempty"`
	Content     string `json:"content"`
	Truncated   bool   `json:"truncated,omitempty"`
}

// NewWebFetch registers tool/web-fetch: Fetch a URL (tool name: web_fetch) and return its readable text.
//
// Best practices:
//   - Needs only web/http-fetch, so it works without any API key.
//   - Pair with tool/web-search: search returns snippets, this returns the page.
//   - A refused or failed fetch comes back as a readable tool result, so the turn survives it.
func NewWebFetch(cfg WebFetchConfig, deps WebFetchDeps) (agentkit.Tool, error) {
	if deps.Web == nil {
		return nil, fmt.Errorf("tool/web-fetch requires web dependency")
	}
	return agentkit.NewTool[WebFetchInput, WebFetchOutput]("web_fetch", func(ctx context.Context, input WebFetchInput) (WebFetchOutput, error) {
		url := strings.TrimSpace(input.URL)
		if url == "" {
			return WebFetchOutput{}, fmt.Errorf("url is required")
		}
		result, err := deps.Web.Fetch(ctx, web.FetchRequest{
			URL:      url,
			MaxBytes: cfg.MaxBytes,
			Raw:      input.Raw,
		})
		if err != nil {
			return WebFetchOutput{}, err
		}
		return WebFetchOutput{
			URL:         result.URL,
			Status:      result.Status,
			ContentType: result.ContentType,
			Title:       result.Title,
			Content:     result.Content,
			Truncated:   result.Truncated,
		}, nil
	}).Description("Fetch a URL and return its readable text. Use it to read a page you already have the address for; cite the returned url when you use the content.").Build()
}

type WebSearchConfig struct {
	// MaxResults is default cap on hits per call when the model does not ask for one.
	MaxResults int `json:"maxResults,omitempty"`
}

type WebSearchDeps struct {
	Web web.Searcher `json:"web"`
}

type WebSearchInput struct {
	Query      string `json:"query" jsonschema:"required,description=What to search for, as a natural-language query"`
	MaxResults int    `json:"maxResults,omitempty" jsonschema:"description=Maximum hits to return"`
}

type WebSearchOutput struct {
	Query    string          `json:"query"`
	Provider string          `json:"provider,omitempty"`
	Results  []web.SearchHit `json:"results"`
}

// NewWebSearch registers tool/web-search: Search the web (tool name: web_search) and return ranked hits with snippets.
//
// Best practices:
//   - Needs a keyed provider such as web/exa-search; use web/scripted-search for tests and smoke runs.
//   - Snippets are excerpts. Fetch the url before relying on any detail.
func NewWebSearch(cfg WebSearchConfig, deps WebSearchDeps) (agentkit.Tool, error) {
	if deps.Web == nil {
		return nil, fmt.Errorf("tool/web-search requires web dependency")
	}
	return agentkit.NewTool[WebSearchInput, WebSearchOutput]("web_search", func(ctx context.Context, input WebSearchInput) (WebSearchOutput, error) {
		query := strings.TrimSpace(input.Query)
		if query == "" {
			return WebSearchOutput{}, fmt.Errorf("query is required")
		}
		maxResults := input.MaxResults
		if maxResults <= 0 {
			maxResults = cfg.MaxResults
		}
		result, err := deps.Web.Search(ctx, web.SearchRequest{Query: query, MaxResults: maxResults})
		if err != nil {
			return WebSearchOutput{}, err
		}
		return WebSearchOutput{
			Query:    result.Query,
			Provider: result.Provider,
			Results:  result.Results,
		}, nil
	}).Description("Search the web and return ranked results with snippets. Snippets are excerpts, not the whole page: fetch a result's url when you need its details.").Build()
}
