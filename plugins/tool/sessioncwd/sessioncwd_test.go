package sessioncwd

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/workspace"
	"github.com/lengzhao/agentkit/runtime/rctx"
	"github.com/lengzhao/agentkit/runtime/session/sessbind"
	"github.com/lengzhao/agentkit/runtime/session/sessctx"
	"github.com/lengzhao/agentkit/runtime/session/sessstore"
	"github.com/lengzhao/agentkit/runtime/tools"
	rtworkspace "github.com/lengzhao/agentkit/runtime/workspace"
)

func fixture(t *testing.T) (agentkit.ToolProvider, *sessstore.Store, workspace.Service, context.Context) {
	t.Helper()
	ws := rtworkspace.Static(t.TempDir())
	store, err := sessstore.NewStore(sessstore.StoreConfig{Dir: "sessions"}, sessstore.StoreDeps{Workspace: ws})
	if err != nil {
		t.Fatal(err)
	}
	p, err := New(Config{}, Deps{SessionStore: store, Workspace: ws})
	if err != nil {
		t.Fatal(err)
	}
	return p, store.(*sessstore.Store), ws, rctx.WithConversation(context.Background(), "test:session")
}
func TestDescriptionAndSetResetAcrossTurns(t *testing.T) {
	p, store, ws, ctx := fixture(t)
	oldDir, newDir := t.TempDir(), t.TempDir()
	if err := store.SetCwdBind(ctx, "test:session", oldDir); err != nil {
		t.Fatal(err)
	}
	ctx, err := sessctx.Prepare(ctx, store, ws)
	if err != nil {
		t.Fatal(err)
	}
	rt, err := tools.NewRuntime(tools.RuntimeConfig{}, tools.RuntimeDeps{DynamicTools: []agentkit.ToolProvider{p}})
	if err != nil {
		t.Fatal(err)
	}
	specs, err := rt.Visible(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(specs) != 1 || specs[0].Name != "session_cwd" || !strings.Contains(specs[0].Description, oldDir) {
		t.Fatalf("specs=%+v", specs)
	}
	raw, _ := json.Marshal(map[string]string{"path": newDir})
	result, err := rt.Execute(ctx, agentkit.ToolCall{ID: "set", Name: "session_cwd", Input: raw})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Content, "next turn") {
		t.Fatalf("result=%s", result.Content)
	}
	if got := workspace.WorkingDirectory(ctx); got != oldDir {
		t.Fatalf("modified current turn: %q", got)
	}
	if got, _ := store.CwdBind(ctx, "test:session"); got != newDir {
		t.Fatalf("binding=%q", got)
	}
	next, err := sessctx.NextTurn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	nextSpecs, err := rt.Visible(next)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(nextSpecs[0].Description, newDir) {
		t.Fatalf("stale description=%s", nextSpecs[0].Description)
	}
	if !strings.Contains(specs[0].Description, oldDir) {
		t.Fatal("old description was mutated")
	}
	_, err = rt.Execute(next, agentkit.ToolCall{ID: "reset", Name: "session_cwd", Input: json.RawMessage(`{"path":""}`)})
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := store.CwdBind(ctx, "test:session"); got != "" {
		t.Fatalf("reset binding=%q", got)
	}
}
func TestRejectInvalidInputWithoutChangingBinding(t *testing.T) {
	p, store, _, ctx := fixture(t)
	valid := t.TempDir()
	if err := store.SetCwdBind(ctx, "test:session", valid); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	list, err := p.ListTools(ctx)
	if err != nil {
		t.Fatal(err)
	}
	cases := []string{`{}`, `{"path":null}`, `{"path":42}`, `{"path":"/missing/session-cwd-test"}`, `null`}
	raw, _ := json.Marshal(map[string]string{"path": file})
	cases = append(cases, string(raw))
	for _, raw := range cases {
		t.Run(raw, func(t *testing.T) {
			if result, err := list[0].Call(ctx, json.RawMessage(raw)); err == nil && strings.Contains(result, "next turn") {
				t.Fatal("expected error")
			}
			if got, _ := store.CwdBind(ctx, "test:session"); got != valid {
				t.Fatalf("binding changed to %q", got)
			}
		})
	}
	if result, err := list[0].Call(context.Background(), json.RawMessage(`{"path":""}`)); err == nil && strings.Contains(result, "next turn") {
		t.Fatal("missing session accepted")
	}
}
func TestCallUsesCallingSessionAndDescriptionsAreIndependent(t *testing.T) {
	p, store, _, ctx := fixture(t)
	a, b := t.TempDir(), t.TempDir()
	ctx = workspace.WithWorkingDirectory(ctx, a)
	other := workspace.WithWorkingDirectory(rctx.WithConversation(context.Background(), "other"), b)
	first, err := p.ListTools(ctx)
	if err != nil {
		t.Fatal(err)
	}
	second, err := p.ListTools(other)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(first[0].Description(), a) || !strings.Contains(second[0].Description(), b) {
		t.Fatal("cwd descriptions mixed")
	}
	raw, _ := json.Marshal(map[string]string{"path": b})
	if _, err := first[0].Call(other, raw); err != nil {
		t.Fatal(err)
	}
	if got, _ := store.CwdBind(ctx, "test:session"); got != "" {
		t.Fatalf("changed listing session: %q", got)
	}
	if got, _ := store.CwdBind(other, "other"); got != b {
		t.Fatalf("calling session binding=%q", got)
	}
}

func TestSchemaRequiresOnlyStringPath(t *testing.T) {
	p, _, _, ctx := fixture(t)
	list, err := p.ListTools(ctx)
	if err != nil {
		t.Fatal(err)
	}
	schema := list[0].InputSchema()
	if len(schema.Properties) != 1 || schema.Properties["path"].Type != "string" || len(schema.Required) != 1 || schema.Required[0] != "path" {
		t.Fatalf("schema=%+v", schema)
	}
}

type unsupportedStore struct{ agentkit.SessionStore }

func TestRequiresDependencies(t *testing.T) {
	_, store, ws, _ := fixture(t)
	for _, deps := range []Deps{{}, {SessionStore: store}, {Workspace: ws}, {SessionStore: unsupportedStore{store}, Workspace: ws}} {
		if _, err := New(Config{}, deps); err == nil {
			t.Fatalf("accepted deps=%+v", deps)
		}
	}
}

func TestResetSessionCwdRevealsChannelBinding(t *testing.T) {
	p, store, ws, _ := fixture(t)
	route := rctx.BuildSessionRoute(agentkit.SessionRouteInput{Platform: "lark", ChannelID: "C1", ThreadID: "T1"})
	env := rctx.ResolveEnvelope(agentkit.MessageEvent{Envelope: agentkit.TurnEnvelope{Route: route}}, rctx.DefaultRoutePolicy(agentkit.SessionScopeThread))
	env.Conversation = "test:session"
	ctx := rctx.ApplyEnvelopeToContext(context.Background(), env)
	channel, session := t.TempDir(), t.TempDir()
	stack := sessbind.StackFromContext(ctx, ws)
	if err := stack.Set(ctx, store, sessbind.CwdBinding{}, sessbind.LevelChannel, channel); err != nil {
		t.Fatal(err)
	}
	if err := store.SetCwdBind(ctx, "test:session", session); err != nil {
		t.Fatal(err)
	}
	empty := ""
	if _, err := p.(*provider).call(ctx, Input{Path: &empty}); err != nil {
		t.Fatalf("reset rejected channel fallback: %v", err)
	}
	effective, _, err := stack.Effective(ctx, store, sessbind.CwdBinding{})
	if err != nil || effective != channel {
		t.Fatalf("fallback = %s, %v", effective, err)
	}
}
