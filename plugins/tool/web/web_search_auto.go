package web

import (
	"context"
	"fmt"
	"strings"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/credentials"
)

type WebSearchAutoConfig struct {
	// MaxResults is cap on hits per call; defaults to 5.
	MaxResults int `json:"maxResults,omitempty"`
	// SnippetChars is snippet truncation limit; defaults to 800.
	SnippetChars int `json:"snippetChars,omitempty"`
	// Tavily holds Tavily provider settings; tried first when a key is available.
	Tavily WebSearchTavilyConfig `json:"tavily,omitempty"`
	// DuckDuckGo holds DuckDuckGo fallback settings.
	DuckDuckGo WebSearchDuckDuckGoConfig `json:"duckduckgo,omitempty"`
}

type WebSearchAutoDeps struct {
	Credentials credentials.Store `json:"credentials,omitempty"`
}

type webSearchAuto struct {
	tavily     *tavilySearcher
	duckduckgo *duckduckgoSearcher
}

// NewWebSearchAuto registers tool/web-search-auto: Tavily when keyed, otherwise DuckDuckGo (tool name: web_search).
func NewWebSearchAuto(cfg WebSearchAutoConfig, deps WebSearchAutoDeps) (agentkit.Tool, error) {
	maxResults := cfg.MaxResults
	if maxResults <= 0 {
		maxResults = defaultTavilyMaxResults
	}
	snippetChars := cfg.SnippetChars
	if snippetChars <= 0 {
		snippetChars = defaultSnippetChars
	}

	tavilyCfg := cfg.Tavily
	if tavilyCfg.MaxResults <= 0 {
		tavilyCfg.MaxResults = maxResults
	}
	if tavilyCfg.SnippetChars <= 0 {
		tavilyCfg.SnippetChars = snippetChars
	}
	ddgCfg := cfg.DuckDuckGo
	if ddgCfg.MaxResults <= 0 {
		ddgCfg.MaxResults = maxResults
	}
	if ddgCfg.SnippetChars <= 0 {
		ddgCfg.SnippetChars = snippetChars
	}

	auto := &webSearchAuto{
		tavily:     newTavilySearcher(tavilyCfg, WebSearchTavilyDeps{Credentials: deps.Credentials}),
		duckduckgo: newDuckDuckGoSearcher(ddgCfg),
	}

	tool, err := agentkit.NewTool[WebSearchInput, WebSearchOutput]("web_search", auto.search).Description("Search the web and return ranked results with snippets. Snippets are excerpts, not the whole page: fetch a result's url when you need its details.").Build()
	if err != nil {
		return nil, err
	}
	return tool, nil
}

func (a *webSearchAuto) search(ctx context.Context, input WebSearchInput) (WebSearchOutput, error) {
	query := strings.TrimSpace(input.Query)
	if query == "" {
		return WebSearchOutput{}, fmt.Errorf("query is required")
	}
	max := input.MaxResults
	if max <= 0 {
		max = a.tavily.maxResults
	}

	if a.tavily.apiKey != "" {
		out, err := a.tavily.search(ctx, query, max)
		if err == nil {
			return out, nil
		}
		if out, ddgErr := a.duckduckgo.search(ctx, query, max); ddgErr == nil {
			return out, nil
		}
		return WebSearchOutput{}, err
	}

	return a.duckduckgo.search(ctx, query, max)
}
