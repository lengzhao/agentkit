package web_test

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/permission"
	"github.com/lengzhao/agentkit/plugins/tool/askuser"
	"github.com/lengzhao/agentkit/plugins/tool/testutil"
	"github.com/lengzhao/agentkit/plugins/tool/web"
)

func TestWebFetchHTTPToolSchema(t *testing.T) {
	t.Parallel()

	fetch, err := web.NewWebFetchHTTP(web.WebFetchHTTPConfig{MaxBytes: 4096})
	if err != nil {
		t.Fatal(err)
	}
	if fetch.Name() != "web_fetch" {
		t.Fatalf("tool name = %q", fetch.Name())
	}
	if !slices.Contains(fetch.InputSchema().Required, "url") {
		t.Errorf("required = %v, want url", fetch.InputSchema().Required)
	}
}

func TestWebFetchScriptedMapsResult(t *testing.T) {
	t.Parallel()

	fetch, err := web.NewWebFetchScripted(web.WebFetchScriptedConfig{
		Pages: map[string]string{
			"example.com": "<html><head><title>Doc</title></head><body>body text</body></html>",
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	out := testutil.CallTool(t, context.Background(), fetch, `{"url":"https://example.com/start","raw":true}`)
	var got web.WebFetchOutput
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("decode output: %v (%s)", err, out)
	}
	if got.URL != "https://example.com/start" {
		t.Errorf("url = %q", got.URL)
	}
	if got.Title != "" {
		t.Errorf("title = %q, want empty when raw=true", got.Title)
	}
	if !strings.Contains(got.Content, "body text") {
		t.Errorf("output = %+v", got)
	}
}

func TestWebFetchHTTPReportsPrivateHostWithoutFailingTurn(t *testing.T) {
	t.Parallel()

	fetch, err := web.NewWebFetchHTTP(web.WebFetchHTTPConfig{})
	if err != nil {
		t.Fatal(err)
	}
	out := testutil.CallTool(t, context.Background(), fetch, `{"url":"http://127.0.0.1/"}`)
	if !strings.Contains(out, "non-public address") {
		t.Errorf("result = %q, want the refusal text", out)
	}
}

func TestWebFetchToolRequiresURL(t *testing.T) {
	t.Parallel()

	fetch, err := web.NewWebFetchScripted(web.WebFetchScriptedConfig{Default: "ok"})
	if err != nil {
		t.Fatal(err)
	}
	if out := testutil.CallTool(t, context.Background(), fetch, `{"url":"  "}`); !strings.Contains(out, "required") {
		t.Errorf("result = %q", out)
	}
}

func TestWebSearchScriptedResultLimit(t *testing.T) {
	t.Parallel()

	search, err := web.NewWebSearchScripted(web.WebSearchScriptedConfig{
		MaxResults: 5,
		ByQuery: map[string][]web.WebSearchHit{
			"loop": {{Title: "Loop", URL: "https://example.com/loop", Snippet: "serialized"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if search.Name() != "web_search" {
		t.Fatalf("tool name = %q", search.Name())
	}
	if !slices.Contains(search.InputSchema().Required, "query") {
		t.Errorf("required = %v, want query", search.InputSchema().Required)
	}

	out := testutil.CallTool(t, context.Background(), search, `{"query":"loop"}`)
	var got web.WebSearchOutput
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("decode output: %v (%s)", err, out)
	}
	if len(got.Results) != 1 || got.Results[0].URL != "https://example.com/loop" || got.Provider != "scripted" {
		t.Errorf("output = %+v", got)
	}

	out = testutil.CallTool(t, context.Background(), search, `{"query":"loop","maxResults":0}`)
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("decode output: %v (%s)", err, out)
	}
	if len(got.Results) != 1 {
		t.Errorf("output = %+v", got)
	}
}

func TestWebSearchTavilyRequiresKeyAtCallTime(t *testing.T) {
	t.Parallel()

	search, err := web.NewWebSearchTavily(web.WebSearchTavilyConfig{}, web.WebSearchTavilyDeps{})
	if err != nil {
		t.Fatal(err)
	}
	out := testutil.CallTool(t, context.Background(), search, `{"query":"anything"}`)
	if !strings.Contains(out, "no API key") {
		t.Errorf("result = %q", out)
	}
}

func TestWebSearchExaRequiresKeyAtCallTime(t *testing.T) {
	t.Parallel()

	search, err := web.NewWebSearchExa(web.WebSearchExaConfig{}, web.WebSearchExaDeps{})
	if err != nil {
		t.Fatal(err)
	}
	out := testutil.CallTool(t, context.Background(), search, `{"query":"anything"}`)
	if !strings.Contains(out, "no API key") {
		t.Errorf("result = %q", out)
	}
}

func TestAskUserToolAnsweredPath(t *testing.T) {
	t.Parallel()

	broker := &fakePermissionBroker{result: permission.Result{
		Outcome: permission.OutcomeResolved,
		Answer:  &permission.QuestionResult{Text: "sqlite", Selected: []int{1}},
	}}
	ask, err := askuser.NewAskUser(askuser.AskUserConfig{}, askuser.AskUserDeps{})
	if err != nil {
		t.Fatal(err)
	}
	if ask.Name() != "ask_user" {
		t.Fatalf("tool name = %q", ask.Name())
	}
	if !slices.Contains(ask.InputSchema().Required, "question") {
		t.Errorf("required = %v, want question", ask.InputSchema().Required)
	}

	ctx := context.WithValue(context.Background(), agentkit.KeySessionControl, broker)
	out := testutil.CallTool(t, ctx, ask, `{"question":"which store?","options":["jsonl","sqlite"],"default":"jsonl"}`)
	var got askuser.AskUserOutput
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("decode output: %v (%s)", err, out)
	}
	if !got.Answered || got.Answer != "sqlite" || got.Selected != 1 {
		t.Errorf("output = %+v", got)
	}
	if got.Guidance != "" {
		t.Errorf("guidance = %q, want none on an answered question", got.Guidance)
	}
	if broker.got.Question == nil || len(broker.got.Question.Options) != 2 || broker.got.Question.Default != "jsonl" {
		t.Errorf("question = %+v", broker.got.Question)
	}
}

func TestAskUserToolUnansweredCarriesGuidance(t *testing.T) {
	t.Parallel()

	broker := &fakePermissionBroker{result: permission.Result{
		Outcome:  permission.OutcomeNoHuman,
		Reason:   "this run is unattended",
		Guidance: "Nobody answered. Do not ask again: pick the most reasonable option yourself, state the assumption you made, and continue.",
	}}
	ask, err := askuser.NewAskUser(askuser.AskUserConfig{}, askuser.AskUserDeps{})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.WithValue(context.Background(), agentkit.KeySessionControl, broker)
	out := testutil.CallTool(t, ctx, ask, `{"question":"which store?"}`)
	var got askuser.AskUserOutput
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("decode output: %v (%s)", err, out)
	}
	if got.Answered {
		t.Errorf("output = %+v, want unanswered", got)
	}
	if !strings.Contains(got.Guidance, "Do not ask again") {
		t.Errorf("guidance = %q", got.Guidance)
	}
	if got.Reason != "no_human: this run is unattended" {
		t.Errorf("reason = %q, want the provider's explanation with outcome", got.Reason)
	}
}

func TestAskUserToolRequiresQuestion(t *testing.T) {
	t.Parallel()

	ask, err := askuser.NewAskUser(askuser.AskUserConfig{}, askuser.AskUserDeps{})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.WithValue(context.Background(), agentkit.KeySessionControl, &fakePermissionBroker{})
	if out := testutil.CallTool(t, ctx, ask, `{"question":"   "}`); !strings.Contains(out, "required") {
		t.Errorf("result = %q", out)
	}
}

type fakePermissionBroker struct {
	got    permission.Request
	result permission.Result
	err    error
}

func (f *fakePermissionBroker) Await(_ context.Context, req permission.Request) (permission.Result, error) {
	f.got = req
	return f.result, f.err
}
