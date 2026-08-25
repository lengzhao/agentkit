package tool_test

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/interaction"
	capweb "github.com/lengzhao/agentkit/cap/web"
	"github.com/lengzhao/agentkit/plugins/tool"
)

type fakeFetcher struct {
	got    capweb.FetchRequest
	result capweb.FetchResult
	err    error
}

func (f *fakeFetcher) Fetch(_ context.Context, req capweb.FetchRequest) (capweb.FetchResult, error) {
	f.got = req
	return f.result, f.err
}

type fakeSearcher struct {
	got    capweb.SearchRequest
	result capweb.SearchResult
	err    error
}

func (f *fakeSearcher) Search(_ context.Context, req capweb.SearchRequest) (capweb.SearchResult, error) {
	f.got = req
	return f.result, f.err
}

type fakeSessionInteraction struct {
	got    interaction.Human
	result interaction.Result
	err    error
}

func (f *fakeSessionInteraction) Run(_ context.Context, req interaction.Human) (interaction.Result, error) {
	f.got = req
	return f.result, f.err
}

func TestWebFetchToolMapsResultAndCapsBytes(t *testing.T) {
	t.Parallel()

	fetcher := &fakeFetcher{result: capweb.FetchResult{
		URL:         "https://example.com/final",
		Status:      200,
		ContentType: "text/html",
		Title:       "Doc",
		Content:     "body text",
		Truncated:   true,
	}}
	fetch, err := tool.NewWebFetch(tool.WebFetchConfig{MaxBytes: 4096}, tool.WebFetchDeps{Web: fetcher})
	if err != nil {
		t.Fatal(err)
	}
	if fetch.Name() != "web_fetch" {
		t.Fatalf("tool name = %q", fetch.Name())
	}
	if !slices.Contains(fetch.InputSchema().Required, "url") {
		t.Errorf("required = %v, want url", fetch.InputSchema().Required)
	}

	out := callTool(t, context.Background(), fetch, `{"url":"https://example.com/start","raw":true}`)
	var got tool.WebFetchOutput
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("decode output: %v (%s)", err, out)
	}
	if got.URL != "https://example.com/final" {
		t.Errorf("url = %q, want the post-redirect url so the model cites the right source", got.URL)
	}
	if got.Title != "Doc" || got.Content != "body text" || !got.Truncated {
		t.Errorf("output = %+v", got)
	}
	if fetcher.got.MaxBytes != 4096 || !fetcher.got.Raw {
		t.Errorf("request = %+v, want the config cap and raw passed through", fetcher.got)
	}
}

func TestWebFetchToolReportsErrorWithoutFailingTheTurn(t *testing.T) {
	t.Parallel()

	fetch, err := tool.NewWebFetch(tool.WebFetchConfig{}, tool.WebFetchDeps{
		Web: &fakeFetcher{err: fmt.Errorf("refusing to fetch non-public address 127.0.0.1")},
	})
	if err != nil {
		t.Fatal(err)
	}
	// callTool fails the test if Call returns an error, so reaching the assertion
	// is itself the proof that a refused fetch stays a readable tool result.
	out := callTool(t, context.Background(), fetch, `{"url":"http://127.0.0.1/"}`)
	if !strings.Contains(out, "non-public address") {
		t.Errorf("result = %q, want the refusal text", out)
	}
}

func TestWebFetchToolRequiresURL(t *testing.T) {
	t.Parallel()

	fetch, err := tool.NewWebFetch(tool.WebFetchConfig{}, tool.WebFetchDeps{Web: &fakeFetcher{}})
	if err != nil {
		t.Fatal(err)
	}
	if out := callTool(t, context.Background(), fetch, `{"url":"  "}`); !strings.Contains(out, "required") {
		t.Errorf("result = %q", out)
	}
}

func TestWebFetchToolRequiresDep(t *testing.T) {
	t.Parallel()

	if _, err := tool.NewWebFetch(tool.WebFetchConfig{}, tool.WebFetchDeps{}); err == nil {
		t.Fatal("built without a web dep")
	}
	if _, err := tool.NewWebSearch(tool.WebSearchConfig{}, tool.WebSearchDeps{}); err == nil {
		t.Fatal("built without a web dep")
	}
}

func TestWebSearchToolResultLimitPrecedence(t *testing.T) {
	t.Parallel()

	searcher := &fakeSearcher{result: capweb.SearchResult{
		Query:    "loop",
		Provider: "exa",
		Results:  []capweb.SearchHit{{Title: "Loop", URL: "https://example.com/loop", Snippet: "serialized"}},
	}}
	search, err := tool.NewWebSearch(tool.WebSearchConfig{MaxResults: 5}, tool.WebSearchDeps{Web: searcher})
	if err != nil {
		t.Fatal(err)
	}
	if search.Name() != "web_search" {
		t.Fatalf("tool name = %q", search.Name())
	}
	if !slices.Contains(search.InputSchema().Required, "query") {
		t.Errorf("required = %v, want query", search.InputSchema().Required)
	}

	out := callTool(t, context.Background(), search, `{"query":"loop"}`)
	var got tool.WebSearchOutput
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("decode output: %v (%s)", err, out)
	}
	if len(got.Results) != 1 || got.Results[0].URL != "https://example.com/loop" || got.Provider != "exa" {
		t.Errorf("output = %+v", got)
	}
	if searcher.got.MaxResults != 5 {
		t.Errorf("maxResults = %d, want the config default when the model omits one", searcher.got.MaxResults)
	}

	callTool(t, context.Background(), search, `{"query":"loop","maxResults":2}`)
	if searcher.got.MaxResults != 2 {
		t.Errorf("maxResults = %d, want the model's explicit value to win", searcher.got.MaxResults)
	}
}

func TestAskUserToolAnsweredPath(t *testing.T) {
	t.Parallel()

	si := &fakeSessionInteraction{result: interaction.Result{Answered: true, Text: "sqlite", Selected: 1}}
	ask, err := tool.NewAskUser(tool.AskUserConfig{}, tool.AskUserDeps{})
	if err != nil {
		t.Fatal(err)
	}
	if ask.Name() != "ask_user" {
		t.Fatalf("tool name = %q", ask.Name())
	}
	if !slices.Contains(ask.InputSchema().Required, "question") {
		t.Errorf("required = %v, want question", ask.InputSchema().Required)
	}

	ctx := context.WithValue(context.Background(), agentkit.KeySessionControl, si)
	out := callTool(t, ctx, ask, `{"question":"which store?","options":["jsonl","sqlite"],"default":"jsonl"}`)
	var got tool.AskUserOutput
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("decode output: %v (%s)", err, out)
	}
	if !got.Answered || got.Answer != "sqlite" || got.Selected != 1 {
		t.Errorf("output = %+v", got)
	}
	if got.Guidance != "" {
		t.Errorf("guidance = %q, want none on an answered question", got.Guidance)
	}
	if len(si.got.Options) != 2 || si.got.Default != "jsonl" {
		t.Errorf("question = %+v", si.got)
	}
}

func TestAskUserToolUnansweredCarriesGuidance(t *testing.T) {
	t.Parallel()

	si := &fakeSessionInteraction{result: interaction.Result{Selected: -1, Reason: "this run is unattended"}}
	ask, err := tool.NewAskUser(tool.AskUserConfig{}, tool.AskUserDeps{})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.WithValue(context.Background(), agentkit.KeySessionControl, si)
	out := callTool(t, ctx, ask, `{"question":"which store?"}`)
	var got tool.AskUserOutput
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("decode output: %v (%s)", err, out)
	}
	if got.Answered {
		t.Errorf("output = %+v, want unanswered", got)
	}
	if !strings.Contains(got.Guidance, "Do not ask again") {
		t.Errorf("guidance = %q", got.Guidance)
	}
	if got.Reason != "this run is unattended" {
		t.Errorf("reason = %q, want the provider's explanation", got.Reason)
	}
}

func TestAskUserToolRequiresQuestion(t *testing.T) {
	t.Parallel()

	ask, err := tool.NewAskUser(tool.AskUserConfig{}, tool.AskUserDeps{})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.WithValue(context.Background(), agentkit.KeySessionControl, &fakeSessionInteraction{})
	if out := callTool(t, ctx, ask, `{"question":"   "}`); !strings.Contains(out, "required") {
		t.Errorf("result = %q", out)
	}
}
