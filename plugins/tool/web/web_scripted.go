package web

import (
	"context"
	"fmt"
	"strings"

	"github.com/lengzhao/agentkit"
)

type WebSearchScriptedConfig struct {
	// Results are hits returned for any query ByQuery does not match.
	Results []WebSearchHit `json:"results,omitempty"`
	// ByQuery maps case-insensitive query substring to hits; keep keys mutually exclusive.
	ByQuery map[string][]WebSearchHit `json:"byQuery,omitempty"`
	// MaxResults is default cap on hits per call when the model does not ask for one.
	MaxResults int `json:"maxResults,omitempty"`
}

// NewWebSearchScripted registers tool/web-search-scripted: Canned search results for tests and keyless smoke runs.
func NewWebSearchScripted(cfg WebSearchScriptedConfig) (agentkit.ToolPack, error) {
	maxResults := cfg.MaxResults
	if maxResults <= 0 {
		maxResults = defaultExaMaxResults
	}
	tool, err := agentkit.NewTool[WebSearchInput, WebSearchOutput]("web_search", func(_ context.Context, input WebSearchInput) (WebSearchOutput, error) {
		query := strings.TrimSpace(input.Query)
		if query == "" {
			return WebSearchOutput{}, fmt.Errorf("query is required")
		}
		hits := cfg.Results
		lower := strings.ToLower(query)
		for key, scripted := range cfg.ByQuery {
			if strings.Contains(lower, strings.ToLower(key)) {
				hits = scripted
				break
			}
		}
		limit := input.MaxResults
		if limit <= 0 {
			limit = maxResults
		}
		if limit > 0 && len(hits) > limit {
			hits = hits[:limit]
		}
		return WebSearchOutput{Query: query, Provider: "scripted", Results: hits}, nil
	}).Description("Search the web and return ranked results with snippets. Snippets are excerpts, not the whole page: fetch a result's url when you need its details.").Build()
	if err != nil {
		return nil, err
	}
	return agentkit.Pack(tool), nil
}

type WebFetchScriptedConfig struct {
	// Pages maps URL substring to the HTML served for it.
	Pages map[string]string `json:"pages,omitempty"`
	// Default is body served when no Pages key matches; empty means not found.
	Default string `json:"default,omitempty"`
}

// NewWebFetchScripted registers tool/web-fetch-scripted: Canned page bodies for tests and keyless smoke runs.
func NewWebFetchScripted(cfg WebFetchScriptedConfig) (agentkit.ToolPack, error) {
	tool, err := agentkit.NewTool[WebFetchInput, WebFetchOutput]("web_fetch", func(_ context.Context, input WebFetchInput) (WebFetchOutput, error) {
		target := strings.TrimSpace(input.URL)
		if target == "" {
			return WebFetchOutput{}, fmt.Errorf("url is required")
		}
		body := cfg.Default
		for key, page := range cfg.Pages {
			if strings.Contains(target, key) {
				body = page
				break
			}
		}
		if body == "" {
			return WebFetchOutput{}, fmt.Errorf("scripted fetch has no page for %q", target)
		}
		result := WebFetchOutput{
			URL:         target,
			Status:      200,
			ContentType: "text/html; charset=utf-8",
		}
		if input.Raw {
			result.Content = body
			return result, nil
		}
		result.Title = extractTitle(body)
		result.Content = htmlToText(body)
		return result, nil
	}).Description("Fetch a URL and return its readable text. Use it to read a page you already have the address for; cite the returned url when you use the content.").Build()
	if err != nil {
		return nil, err
	}
	return agentkit.Pack(tool), nil
}
