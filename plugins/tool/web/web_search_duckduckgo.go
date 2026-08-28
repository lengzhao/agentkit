package web

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/lengzhao/agentkit"
)

const (
	defaultDuckDuckGoMaxResults = 5
	duckDuckGoSearchURL         = "https://html.duckduckgo.com/html/"
)

var (
	reDDGLink = regexp.MustCompile(
		`<a[^>]*class="[^"]*result__a[^"]*"[^>]*href="([^"]+)"[^>]*>([\s\S]*?)</a>`,
	)
	reDDGSnippet = regexp.MustCompile(
		`<a class="result__snippet[^"]*".*?>([\s\S]*?)</a>`,
	)
)

type WebSearchDuckDuckGoConfig struct {
	// MaxResults is cap on hits per call; defaults to 5.
	MaxResults int `json:"maxResults,omitempty"`
	// TimeoutSeconds is per-request wall clock; defaults to 30.
	TimeoutSeconds int `json:"timeoutSeconds,omitempty"`
	// SnippetChars is snippet truncation limit; defaults to 800.
	SnippetChars int `json:"snippetChars,omitempty"`
}

type duckduckgoSearcher struct {
	client       *http.Client
	maxResults   int
	snippetChars int
}

// NewWebSearchDuckDuckGo registers tool/web-search-duckduckgo: Search via DuckDuckGo HTML (tool name: web_search).
//
// No API key is required. Results come from html.duckduckgo.com and may be rate-limited by DuckDuckGo.
func NewWebSearchDuckDuckGo(cfg WebSearchDuckDuckGoConfig) (agentkit.ToolPack, error) {
	d := newDuckDuckGoSearcher(cfg)

	tool, err := agentkit.NewTool[WebSearchInput, WebSearchOutput]("web_search", func(ctx context.Context, input WebSearchInput) (WebSearchOutput, error) {
		query := strings.TrimSpace(input.Query)
		if query == "" {
			return WebSearchOutput{}, fmt.Errorf("query is required")
		}
		max := input.MaxResults
		if max <= 0 {
			max = d.maxResults
		}
		return d.search(ctx, query, max)
	}).Description("Search the web and return ranked results with snippets. Snippets are excerpts, not the whole page: fetch a result's url when you need its details.").Build()
	if err != nil {
		return nil, err
	}
	return agentkit.Pack(tool), nil
}

func newDuckDuckGoSearcher(cfg WebSearchDuckDuckGoConfig) *duckduckgoSearcher {
	timeout := 30 * time.Second
	if cfg.TimeoutSeconds > 0 {
		timeout = time.Duration(cfg.TimeoutSeconds) * time.Second
	}
	maxResults := cfg.MaxResults
	if maxResults <= 0 {
		maxResults = defaultDuckDuckGoMaxResults
	}
	snippetChars := cfg.SnippetChars
	if snippetChars <= 0 {
		snippetChars = defaultSnippetChars
	}
	return &duckduckgoSearcher{
		client:       &http.Client{Timeout: timeout},
		maxResults:   maxResults,
		snippetChars: snippetChars,
	}
}

func (d *duckduckgoSearcher) search(ctx context.Context, query string, maxResults int) (WebSearchOutput, error) {
	n := maxResults
	if n <= 0 || n > d.maxResults {
		n = d.maxResults
	}

	searchURL := duckDuckGoSearchURL + "?q=" + url.QueryEscape(query)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, searchURL, nil)
	if err != nil {
		return WebSearchOutput{}, err
	}
	req.Header.Set("User-Agent", duckDuckGoUserAgent)

	resp, err := d.client.Do(req)
	if err != nil {
		return WebSearchOutput{}, fmt.Errorf("duckduckgo search: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return WebSearchOutput{}, fmt.Errorf("duckduckgo search: read response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return WebSearchOutput{}, fmt.Errorf("duckduckgo search: http %d: %s", resp.StatusCode, truncateText(strings.TrimSpace(string(body)), 300))
	}

	hits := parseDuckDuckGoHTML(string(body), n, d.snippetChars)
	out := WebSearchOutput{Query: query, Provider: "duckduckgo", Results: hits}
	if len(hits) == 0 {
		return out, fmt.Errorf("duckduckgo search: no results extracted for %q", query)
	}
	return out, nil
}

func parseDuckDuckGoHTML(doc string, maxResults, snippetChars int) []WebSearchHit {
	links := reDDGLink.FindAllStringSubmatch(doc, maxResults+5)
	if len(links) == 0 {
		return nil
	}
	snippets := reDDGSnippet.FindAllStringSubmatch(doc, maxResults+5)

	limit := maxResults
	if limit > len(links) {
		limit = len(links)
	}
	out := make([]WebSearchHit, 0, limit)
	for i := range limit {
		target := decodeDuckDuckGoURL(links[i][1])
		title := collapse(stripHTMLTags(links[i][2]))
		snippet := ""
		if i < len(snippets) {
			snippet = truncateText(collapse(stripHTMLTags(snippets[i][1])), snippetChars)
		}
		out = append(out, WebSearchHit{
			Title:   strings.TrimSpace(title),
			URL:     target,
			Snippet: snippet,
		})
	}
	return out
}

func decodeDuckDuckGoURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	decoded, err := url.QueryUnescape(raw)
	if err != nil {
		decoded = raw
	}
	if _, after, ok := strings.Cut(decoded, "uddg="); ok {
		if target, err := url.QueryUnescape(after); err == nil && target != "" {
			return target
		}
		return after
	}
	return decoded
}

func stripHTMLTags(s string) string {
	s = reScript.ReplaceAllString(s, "")
	s = reStyle.ReplaceAllString(s, "")
	s = reTags.ReplaceAllString(s, " ")
	return decodeEntities(s)
}

const duckDuckGoUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"

var (
	reScript = regexp.MustCompile(`(?is)<script[\s\S]*?</script>`)
	reStyle  = regexp.MustCompile(`(?is)<style[\s\S]*?</style>`)
	reTags   = regexp.MustCompile(`(?is)<[^>]+>`)
)
