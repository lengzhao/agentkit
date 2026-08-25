package web

import (
	"context"
	"fmt"
	"strings"

	"github.com/lengzhao/agentkit/cap/web"
	"github.com/lengzhao/pluginkit"
)

// Scripted providers are the network-free counterparts of llm/scripted: they
// let a preset exercise the search-then-fetch loop, and tests assert on tool
// wiring, without an API key or a live host.

type ScriptedSearchConfig struct {
	// Results is returned for any query not matched by ByQuery.
	Results []web.SearchHit `json:"results,omitempty"`
	// ByQuery maps a case-insensitive query substring to its hits. Map
	// iteration order is random, so keep the keys mutually exclusive.
	ByQuery map[string][]web.SearchHit `json:"byQuery,omitempty"`
}

type scriptedSearcher struct{ cfg ScriptedSearchConfig }

func init() {
	pluginkit.Register("web/scripted-search", NewScriptedSearch)
	pluginkit.Register("web/scripted-fetch", NewScriptedFetch)
}

func NewScriptedSearch(cfg ScriptedSearchConfig) (web.Searcher, error) {
	return &scriptedSearcher{cfg: cfg}, nil
}

func (s *scriptedSearcher) Search(_ context.Context, req web.SearchRequest) (web.SearchResult, error) {
	query := strings.TrimSpace(req.Query)
	if query == "" {
		return web.SearchResult{}, fmt.Errorf("query is required")
	}
	hits := s.cfg.Results
	lower := strings.ToLower(query)
	for key, scripted := range s.cfg.ByQuery {
		if strings.Contains(lower, strings.ToLower(key)) {
			hits = scripted
			break
		}
	}
	if req.MaxResults > 0 && len(hits) > req.MaxResults {
		hits = hits[:req.MaxResults]
	}
	return web.SearchResult{Query: query, Provider: "scripted", Results: hits}, nil
}

type ScriptedFetchConfig struct {
	// Pages maps a URL substring to the content served for it.
	Pages map[string]string `json:"pages,omitempty"`
	// Default is served for URLs no key matches. Empty means "not found".
	Default string `json:"default,omitempty"`
}

type scriptedFetcher struct{ cfg ScriptedFetchConfig }

func NewScriptedFetch(cfg ScriptedFetchConfig) (web.Fetcher, error) {
	return &scriptedFetcher{cfg: cfg}, nil
}

func (s *scriptedFetcher) Fetch(_ context.Context, req web.FetchRequest) (web.FetchResult, error) {
	target := strings.TrimSpace(req.URL)
	if target == "" {
		return web.FetchResult{}, fmt.Errorf("url is required")
	}
	body := s.cfg.Default
	for key, page := range s.cfg.Pages {
		if strings.Contains(target, key) {
			body = page
			break
		}
	}
	if body == "" {
		return web.FetchResult{}, fmt.Errorf("scripted fetch has no page for %q", target)
	}
	result := web.FetchResult{
		URL:         target,
		Status:      200,
		ContentType: "text/html; charset=utf-8",
		Bytes:       len(body),
	}
	if req.Raw {
		result.Content = body
		return result, nil
	}
	result.Title = extractTitle(body)
	result.Content = htmlToText(body)
	return result, nil
}
