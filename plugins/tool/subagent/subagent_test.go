package subagent_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/lengzhao/agentkit"
	capsubagent "github.com/lengzhao/agentkit/cap/subagent"
	"github.com/lengzhao/agentkit/plugins/tool/subagent"
	"github.com/lengzhao/agentkit/plugins/tool/testutil"
)

type fakeSpawner struct {
	got    capsubagent.Request
	result capsubagent.Result
	err    error
}

func (f *fakeSpawner) Definitions(context.Context) ([]capsubagent.Definition, error) {
	return nil, nil
}

func (f *fakeSpawner) Run(_ context.Context, req capsubagent.Request) (capsubagent.Result, error) {
	f.got = req
	return f.result, f.err
}

func TestSubagentToolReturnsSummary(t *testing.T) {
	t.Parallel()

	spawner := &fakeSpawner{result: capsubagent.Result{
		Agent:   "researcher",
		Session: "sub:cli:default:researcher:12",
		Status:  capsubagent.StatusCompleted,
		Summary: "the loop serializes turns per session",
		Steps:   4,
	}}
	delegatePack, err := subagent.NewSubagent(subagent.SubagentConfig{}, subagent.SubagentDeps{Subagent: spawner})
	if err != nil {
		t.Fatal(err)
	}
	delegate := agentkit.First(delegatePack)
	if delegate.Name() != "delegate" {
		t.Fatalf("tool name = %q, want delegate", delegate.Name())
	}

	out := testutil.CallTool(t, context.Background(), delegate, `{"agent":"researcher","task":"how does the loop serialize turns?"}`)
	var got subagent.SubagentOutput
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("decode output: %v (%s)", err, out)
	}
	if got.Summary != "the loop serializes turns per session" {
		t.Errorf("summary = %q", got.Summary)
	}
	if got.Status != capsubagent.StatusCompleted || got.Steps != 4 {
		t.Errorf("output = %+v", got)
	}
	if got.Session != "sub:cli:default:researcher:12" {
		t.Errorf("session = %q, want the child session id for auditing", got.Session)
	}
	if spawner.got.Agent != "researcher" || !strings.Contains(spawner.got.Task, "serialize turns") {
		t.Errorf("spawner request = %+v", spawner.got)
	}
}

func TestSubagentToolSurfacesSpawnerError(t *testing.T) {
	t.Parallel()

	spawner := &fakeSpawner{err: fmt.Errorf("unknown subagent %q; available: researcher", "reviewer")}
	delegatePack, err := subagent.NewSubagent(subagent.SubagentConfig{}, subagent.SubagentDeps{Subagent: spawner})
	if err != nil {
		t.Fatal(err)
	}
	delegate := agentkit.First(delegatePack)

	// The tool builder turns a handler error into a text result, so the model can
	// read the available names and retry instead of the turn dying.
	out := testutil.CallTool(t, context.Background(), delegate, `{"agent":"reviewer","task":"review"}`)
	if !strings.Contains(out, "available: researcher") {
		t.Fatalf("output = %q, want the spawner's error text", out)
	}
}

func TestSubagentToolSchemaMarksBothFieldsRequired(t *testing.T) {
	t.Parallel()

	delegatePack, err := subagent.NewSubagent(subagent.SubagentConfig{}, subagent.SubagentDeps{Subagent: &fakeSpawner{}})
	if err != nil {
		t.Fatal(err)
	}
	delegate := agentkit.First(delegatePack)
	schema, err := json.Marshal(delegate.InputSchema())
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Required []string `json:"required"`
	}
	if err := json.Unmarshal(schema, &decoded); err != nil {
		t.Fatalf("decode schema: %v (%s)", err, schema)
	}
	want := map[string]bool{"agent": true, "task": true}
	for _, name := range decoded.Required {
		delete(want, name)
	}
	if len(want) != 0 {
		t.Fatalf("schema required = %v, missing %v\nschema: %s", decoded.Required, want, schema)
	}
}

func TestSubagentToolRequiresSpawner(t *testing.T) {
	t.Parallel()

	if _, err := subagent.NewSubagent(subagent.SubagentConfig{}, subagent.SubagentDeps{}); err == nil {
		t.Fatal("expected an error without a spawner dependency")
	}
}
