package web

import "testing"

func TestParseDuckDuckGoHTML(t *testing.T) {
	t.Parallel()

	hits := parseDuckDuckGoHTML(`<a class="result__a" href="//duckduckgo.com/l/?uddg=https%3A%2F%2Fexample.com%2Floop">Loop docs</a><a class="result__snippet">session lock</a>`, 5, 200)
	if len(hits) != 1 {
		t.Fatalf("len = %d", len(hits))
	}
	if hits[0].URL != "https://example.com/loop" || hits[0].Title != "Loop docs" || hits[0].Snippet != "session lock" {
		t.Fatalf("hits = %+v", hits)
	}
}

func TestDecodeDuckDuckGoURL(t *testing.T) {
	t.Parallel()

	got := decodeDuckDuckGoURL("//duckduckgo.com/l/?uddg=https%3A%2F%2Fexample.com%2Fpage")
	if got != "https://example.com/page" {
		t.Fatalf("url = %q", got)
	}
}

func TestWebSearchAutoSkipsTavilyWithoutKey(t *testing.T) {
	t.Parallel()

	auto := &webSearchAuto{
		tavily:     &tavilySearcher{apiKey: "", maxResults: 5},
		duckduckgo: &duckduckgoSearcher{maxResults: 5},
	}
	if auto.tavily.apiKey != "" {
		t.Fatal("expected empty tavily key in fallback setup")
	}
}
