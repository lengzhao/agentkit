package tool_test

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/interaction"
	"github.com/lengzhao/agentkit/plugins/tool"
)

func TestWebFetchHTTPToolSchema(t *testing.T) {
	t.Parallel()

	pack, err := tool.NewWebFetchHTTP(tool.WebFetchHTTPConfig{MaxBytes: 4096})
	if err != nil {
		t.Fatal(err)
	}
	fetch := agentkit.First(pack)
	if fetch.Name() != "web_fetch" {
		t.Fatalf("tool name = %q", fetch.Name())
	}
	if !slices.Contains(fetch.InputSchema().Required, "url") {
		t.Errorf("required = %v, want url", fetch.InputSchema().Required)
	}
}

func TestWebFetchScriptedMapsResult(t *testing.T) {
	t.Parallel()

	pack, err := tool.NewWebFetchScripted(tool.WebFetchScriptedConfig{
		Pages: map[string]string{
			"example.com": "<html><head><title>Doc</title></head><body>body text</body></html>",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	fetch := agentkit.First(pack)

	out := callTool(t, context.Background(), fetch, `{"url":"https://example.com/start","raw":true}`)
	var got tool.WebFetchOutput
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

	pack, err := tool.NewWebFetchHTTP(tool.WebFetchHTTPConfig{})
	if err != nil {
		t.Fatal(err)
	}
	fetch := agentkit.First(pack)
	out := callTool(t, context.Background(), fetch, `{"url":"http://127.0.0.1/"}`)
	if !strings.Contains(out, "non-public address") {
		t.Errorf("result = %q, want the refusal text", out)
	}
}

func TestWebFetchToolRequiresURL(t *testing.T) {
	t.Parallel()

	pack, err := tool.NewWebFetchScripted(tool.WebFetchScriptedConfig{Default: "ok"})
	if err != nil {
		t.Fatal(err)
	}
	fetch := agentkit.First(pack)
	if out := callTool(t, context.Background(), fetch, `{"url":"  "}`); !strings.Contains(out, "required") {
		t.Errorf("result = %q", out)
	}
}

func TestWebSearchScriptedResultLimit(t *testing.T) {
	t.Parallel()

	pack, err := tool.NewWebSearchScripted(tool.WebSearchScriptedConfig{
		MaxResults: 5,
		ByQuery: map[string][]tool.WebSearchHit{
			"loop": {{Title: "Loop", URL: "https://example.com/loop", Snippet: "serialized"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	search := agentkit.First(pack)
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
	if len(got.Results) != 1 || got.Results[0].URL != "https://example.com/loop" || got.Provider != "scripted" {
		t.Errorf("output = %+v", got)
	}

	out = callTool(t, context.Background(), search, `{"query":"loop","maxResults":0}`)
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("decode output: %v (%s)", err, out)
	}
	if len(got.Results) != 1 {
		t.Errorf("output = %+v", got)
	}
}

func TestWebSearchExaRequiresKeyAtCallTime(t *testing.T) {
	t.Parallel()

	pack, err := tool.NewWebSearchExa(tool.WebSearchExaConfig{}, tool.WebSearchExaDeps{})
	if err != nil {
		t.Fatal(err)
	}
	search := agentkit.First(pack)
	out := callTool(t, context.Background(), search, `{"query":"anything"}`)
	if !strings.Contains(out, "no API key") {
		t.Errorf("result = %q", out)
	}
}

func TestAskUserToolAnsweredPath(t *testing.T) {
	t.Parallel()

	si := &fakeSessionInteraction{result: interaction.Result{Answered: true, Text: "sqlite", Selected: 1}}
	pack, err := tool.NewAskUser(tool.AskUserConfig{}, tool.AskUserDeps{})
	if err != nil {
		t.Fatal(err)
	}
	ask := agentkit.First(pack)
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
	pack, err := tool.NewAskUser(tool.AskUserConfig{}, tool.AskUserDeps{})
	if err != nil {
		t.Fatal(err)
	}
	ask := agentkit.First(pack)
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

	pack, err := tool.NewAskUser(tool.AskUserConfig{}, tool.AskUserDeps{})
	if err != nil {
		t.Fatal(err)
	}
	ask := agentkit.First(pack)
	ctx := context.WithValue(context.Background(), agentkit.KeySessionControl, &fakeSessionInteraction{})
	if out := callTool(t, ctx, ask, `{"question":"   "}`); !strings.Contains(out, "required") {
		t.Errorf("result = %q", out)
	}
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
