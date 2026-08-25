// Package web is the capability boundary for reaching the network: fetching a
// URL and running a search. Fetching and searching are separate interfaces on
// purpose — an HTTP fetcher needs no credentials and must stay usable on its
// own, while every search provider needs an API key.
package web

import "context"

// Fetcher retrieves a single URL.
type Fetcher interface {
	Fetch(context.Context, FetchRequest) (FetchResult, error)
}

// Searcher answers a query with ranked hits.
type Searcher interface {
	Search(context.Context, SearchRequest) (SearchResult, error)
}

type FetchRequest struct {
	URL string `json:"url"`
	// MaxBytes overrides the provider default for this call. 0 = provider default.
	MaxBytes int `json:"maxBytes,omitempty"`
	// Raw skips HTML-to-text extraction and returns the body as served.
	Raw bool `json:"raw,omitempty"`
}

type FetchResult struct {
	// URL is the final URL, after redirects.
	URL         string `json:"url"`
	Status      int    `json:"status"`
	ContentType string `json:"contentType,omitempty"`
	Title       string `json:"title,omitempty"`
	Content     string `json:"content"`
	// Bytes is the size of the body actually read, before extraction.
	Bytes     int  `json:"bytes"`
	Truncated bool `json:"truncated,omitempty"`
}

type SearchRequest struct {
	Query string `json:"query"`
	// MaxResults caps the hits returned. 0 = provider default.
	MaxResults int `json:"maxResults,omitempty"`
}

type SearchResult struct {
	Query    string      `json:"query"`
	Results  []SearchHit `json:"results"`
	Provider string      `json:"provider,omitempty"`
}

type SearchHit struct {
	Title string `json:"title"`
	URL   string `json:"url"`
	// Snippet is a query-relevant excerpt, not the whole page.
	Snippet     string `json:"snippet,omitempty"`
	PublishedAt string `json:"publishedAt,omitempty"`
}
