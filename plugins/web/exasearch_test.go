package web

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	capweb "github.com/lengzhao/agentkit/cap/web"
)

func TestExaSearchSendsWireShapeAndMapsResults(t *testing.T) {
	t.Parallel()

	var gotKey string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/search" || r.Method != http.MethodPost {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		gotKey = r.Header.Get("x-api-key")
		raw, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(raw, &gotBody); err != nil {
			t.Errorf("decode request: %v (%s)", err, raw)
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"results":[
			{"title":"Loop docs","url":"https://example.com/loop","publishedDate":"2026-01-02","highlights":["turns are serialized","per session"]},
			{"title":"No highlights","url":"https://example.com/b","text":"  fallback   text  "}
		]}`)
	}))
	defer srv.Close()

	searcher, err := NewExa(ExaConfig{APIKey: "k-123", BaseURL: srv.URL, MaxResults: 7}, ExaDeps{})
	if err != nil {
		t.Fatal(err)
	}
	got, err := searcher.Search(context.Background(), capweb.SearchRequest{Query: "how does the loop serialize turns"})
	if err != nil {
		t.Fatal(err)
	}

	if gotKey != "k-123" {
		t.Errorf("x-api-key = %q", gotKey)
	}
	// Exa's wire format is camelCase; snake_case is a Python-SDK convention.
	if _, ok := gotBody["numResults"]; !ok {
		t.Errorf("request body = %v, want camelCase numResults", gotBody)
	}
	if n, _ := gotBody["numResults"].(float64); n != 7 {
		t.Errorf("numResults = %v, want the configured cap", gotBody["numResults"])
	}
	contents, _ := gotBody["contents"].(map[string]any)
	if highlights, _ := contents["highlights"].(bool); !highlights {
		t.Errorf("contents = %v, want highlights requested", contents)
	}
	if _, ok := contents["text"]; ok {
		t.Errorf("contents = %v, want no text unless includeText is set", contents)
	}

	if len(got.Results) != 2 || got.Provider != "exa" {
		t.Fatalf("result = %+v", got)
	}
	if got.Results[0].Snippet != "turns are serialized … per session" {
		t.Errorf("snippet = %q, want joined highlights", got.Results[0].Snippet)
	}
	if got.Results[0].PublishedAt != "2026-01-02" {
		t.Errorf("publishedAt = %q", got.Results[0].PublishedAt)
	}
	if got.Results[1].Snippet != "fallback text" {
		t.Errorf("fallback snippet = %q, want collapsed text", got.Results[1].Snippet)
	}
}

func TestExaSearchCapsRequestedResults(t *testing.T) {
	t.Parallel()

	var numResults float64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		raw, _ := io.ReadAll(r.Body)
		json.Unmarshal(raw, &body)
		numResults, _ = body["numResults"].(float64)
		io.WriteString(w, `{"results":[]}`)
	}))
	defer srv.Close()

	searcher, err := NewExa(ExaConfig{APIKey: "k", BaseURL: srv.URL, MaxResults: 3}, ExaDeps{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := searcher.Search(context.Background(), capweb.SearchRequest{Query: "q", MaxResults: 50}); err != nil {
		t.Fatal(err)
	}
	if numResults != 3 {
		t.Errorf("numResults = %v, want the config cap to bound the model's request", numResults)
	}
}

func TestExaSearchWithoutKeyBuildsAndReportsAtCallTime(t *testing.T) {
	// No t.Parallel: t.Setenv forbids it.
	t.Setenv("EXA_API_KEY", "")
	searcher, err := NewExa(ExaConfig{}, ExaDeps{})
	if err != nil {
		t.Fatalf("building without a key must succeed so a keyless preset still boots: %v", err)
	}
	_, err = searcher.Search(context.Background(), capweb.SearchRequest{Query: "anything"})
	if err == nil || !strings.Contains(err.Error(), "API key") {
		t.Fatalf("err = %v, want a missing-key message", err)
	}
}

func TestExaSearchSurfacesHTTPError(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		io.WriteString(w, `{"error":"bad key"}`)
	}))
	defer srv.Close()

	searcher, err := NewExa(ExaConfig{APIKey: "k", BaseURL: srv.URL}, ExaDeps{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = searcher.Search(context.Background(), capweb.SearchRequest{Query: "q"})
	if err == nil || !strings.Contains(err.Error(), "401") || !strings.Contains(err.Error(), "bad key") {
		t.Fatalf("err = %v, want the status and the body", err)
	}
}

func TestExaSearchRequiresQuery(t *testing.T) {
	t.Parallel()

	searcher, err := NewExa(ExaConfig{APIKey: "k"}, ExaDeps{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := searcher.Search(context.Background(), capweb.SearchRequest{}); err == nil {
		t.Fatal("empty query was accepted")
	}
}

func TestScriptedProviders(t *testing.T) {
	t.Parallel()

	searcher, err := NewScriptedSearch(ScriptedSearchConfig{
		Results: []capweb.SearchHit{{Title: "default", URL: "https://example.com/d"}},
		ByQuery: map[string][]capweb.SearchHit{
			"loop": {{Title: "Loop", URL: "https://example.com/loop"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := searcher.Search(context.Background(), capweb.SearchRequest{Query: "the LOOP model"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Results) != 1 || got.Results[0].Title != "Loop" {
		t.Errorf("byQuery match failed: %+v", got)
	}
	got, err = searcher.Search(context.Background(), capweb.SearchRequest{Query: "something else"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Results) != 1 || got.Results[0].Title != "default" {
		t.Errorf("fallback failed: %+v", got)
	}

	fetcher, err := NewScriptedFetch(ScriptedFetchConfig{
		Pages: map[string]string{"/loop": "<title>Loop</title><p>serialized per session</p>"},
	})
	if err != nil {
		t.Fatal(err)
	}
	page, err := fetcher.Fetch(context.Background(), capweb.FetchRequest{URL: "https://example.com/loop"})
	if err != nil {
		t.Fatal(err)
	}
	if page.Title != "Loop" || !strings.Contains(page.Content, "serialized per session") {
		t.Errorf("page = %+v", page)
	}
	if _, err := fetcher.Fetch(context.Background(), capweb.FetchRequest{URL: "https://example.com/missing"}); err == nil {
		t.Error("unscripted url was served")
	}
}
