package loop_test

import (
	"context"
	"strings"
	"testing"

	"github.com/lengzhao/agentkit"
	rtloop "github.com/lengzhao/agentkit/runtime/loop"
	"github.com/lengzhao/agentkit/runtime/rctx"
	sessstore "github.com/lengzhao/agentkit/runtime/session/sessstore"
	rtworkspace "github.com/lengzhao/agentkit/runtime/workspace"
)

type stubDetailAgent struct {
	id     agentkit.AgentID
	detail string
}

func (s stubDetailAgent) ID() agentkit.AgentID { return s.id }

func (s stubDetailAgent) RunTurn(context.Context, agentkit.TurnInput) error { return nil }

func (s stubDetailAgent) AgentCatalogEntry() string { return s.detail }

func TestAgentHelpCommand(t *testing.T) {
	t.Parallel()
	agents := []agentkit.Agent{
		stubDetailAgent{id: "assistant", detail: "agent \"assistant\"\nkind: agent/coding\nmodel: gpt-5.4"},
		stubDetailAgent{id: "reviewer", detail: "agent \"reviewer\"\nkind: agent/coding"},
	}
	cmd := rtloop.AgentHelpCommand(agents)
	cases := []struct {
		name string
		args []string
		want []string
	}{
		{
			name: "list",
			args: nil,
			want: []string{"Registered agents:", "assistant", "reviewer", "Use /agent <id>", "Use /agent use <id>"},
		},
		{
			name: "detail",
			args: []string{"assistant"},
			want: []string{"agent \"assistant\"", "kind: agent/coding", "model: gpt-5.4"},
		},
		{
			name: "unknown agent",
			args: []string{"no-such-agent"},
			want: []string{"unknown agent"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := cmd.CommandExec(context.Background(), strings.Join(tc.args, " "))
			if tc.name == "unknown agent" {
				if err == nil {
					t.Fatal("expected error")
				}
				out = err.Error()
			} else if err != nil {
				t.Fatal(err)
			}
			for _, want := range tc.want {
				if !strings.Contains(out, want) {
					t.Errorf("missing %q in output:\n%s", want, out)
				}
			}
		})
	}
}

func TestAgentHelpShowsResetCommands(t *testing.T) {
	t.Parallel()
	for _, agents := range [][]agentkit.Agent{nil, {stubDetailAgent{id: "assistant"}}} {
		out, err := rtloop.AgentHelpCommand(agents).CommandExec(context.Background(), "")
		if err != nil {
			t.Fatal(err)
		}
		for _, command := range []string{"/agent reset", "/agent -c reset", "/agent -g reset"} {
			if !strings.Contains(out, command) {
				t.Errorf("missing %q in output:\n%s", command, out)
			}
		}
	}
}

func TestAgentListShowsSessionAgent(t *testing.T) {
	t.Parallel()

	mem, err := sessstore.NewMemory(sessstore.MemoryConfig{ID: "cli:test"})
	if err != nil {
		t.Fatal(err)
	}
	store := sessstore.NewStaticStore(mem)
	agents := []agentkit.Agent{
		stubDetailAgent{id: "assistant"},
		stubDetailAgent{id: "reviewer"},
	}
	cmd := rtloop.AgentCommand(agents, store, "assistant", nil)
	ctx := rctx.ApplyEnvelopeToContext(context.Background(), agentkit.TurnEnvelope{Conversation: "cli:test", Workspace: "cli:test"})

	out, err := cmd.CommandExec(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Active agent: assistant", "Loop default: assistant", "assistant *", "reviewer"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in output:\n%s", want, out)
		}
	}

	if err := store.SetAgentBind(ctx, "cli:test", "reviewer"); err != nil {
		t.Fatal(err)
	}
	out, err = cmd.CommandExec(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Active agent: reviewer", "Session override: reviewer", "Loop default: assistant"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in output:\n%s", want, out)
		}
	}
	if !strings.Contains(out, "reviewer  *") && !strings.Contains(out, "reviewer *") {
		t.Errorf("expected current agent marker on reviewer in output:\n%s", out)
	}
}

func TestAgentGlobalUse(t *testing.T) {
	t.Parallel()

	mem, err := sessstore.NewMemory(sessstore.MemoryConfig{ID: "cli:test"})
	if err != nil {
		t.Fatal(err)
	}
	store := sessstore.NewStaticStore(mem)
	agents := []agentkit.Agent{stubDetailAgent{id: "assistant"}, stubDetailAgent{id: "worker"}}
	ws := rtworkspace.Static(t.TempDir())
	cmd := rtloop.AgentCommand(agents, store, "assistant", ws)
	ctx := rctx.ApplyEnvelopeToContext(context.Background(), agentkit.TurnEnvelope{
		Conversation: "cli:test",
		Workspace:    "cli:test",
	})

	out, err := cmd.CommandExec(ctx, "-g use worker")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "global agent: worker") {
		t.Fatalf("out = %q", out)
	}

	out, err = cmd.CommandExec(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Global override: worker") {
		t.Fatalf("list = %q", out)
	}
}

func TestAgentChannelUse(t *testing.T) {
	t.Parallel()

	mem, err := sessstore.NewMemory(sessstore.MemoryConfig{ID: "slack:C001"})
	if err != nil {
		t.Fatal(err)
	}
	store := sessstore.NewStaticStore(mem)
	agents := []agentkit.Agent{stubDetailAgent{id: "assistant"}, stubDetailAgent{id: "worker"}}
	ws, err := rtworkspace.New(rtworkspace.Config{
		Global: t.TempDir(),
		Local:  t.TempDir(),
		Scope:  "local",
	})
	if err != nil {
		t.Fatal(err)
	}
	cmd := rtloop.AgentCommand(agents, store, "assistant", ws)
	delivery := rctx.BuildDeliverySessionID("slack", "C001", "", "U1")
	env := rctx.ResolveEnvelope(rctx.WithDeliveryRoute(agentkit.MessageEvent{
		PlatformID: "slack",
		UserID:     "U1",
	}, rctx.SessionRoute("slack", string(delivery))), rctx.DefaultRoutePolicy(agentkit.SessionScopeChannel))
	ctx := rctx.ApplyEnvelopeToContext(context.Background(), env)

	out, err := cmd.CommandExec(ctx, "-c use worker")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "channel agent: worker") {
		t.Fatalf("out = %q", out)
	}

	out, err = cmd.CommandExec(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Active agent: worker") || !strings.Contains(out, "Channel override: worker") {
		t.Fatalf("list = %q", out)
	}

	out, err = cmd.CommandExec(ctx, "--channel reset")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "channel agent reset; loop default: assistant") {
		t.Fatalf("reset = %q", out)
	}

	if _, err := cmd.CommandExec(ctx, "-g -c use worker"); err == nil {
		t.Fatal("expected combined flag error")
	}
}

func TestAgentScopePrecedenceAndNewSession(t *testing.T) {
	t.Parallel()
	delivery := rctx.BuildDeliverySessionID("slack", "C001", "", "U1")
	env := rctx.ResolveEnvelope(rctx.WithDeliveryRoute(agentkit.MessageEvent{
		PlatformID: "slack", UserID: "U1",
	}, rctx.SessionRoute("slack", string(delivery))), rctx.DefaultRoutePolicy(agentkit.SessionScopeChannel))
	ctx := rctx.ApplyEnvelopeToContext(context.Background(), env)
	mem, err := sessstore.NewMemory(sessstore.MemoryConfig{ID: agentkit.SessionID(env.Conversation)})
	if err != nil {
		t.Fatal(err)
	}
	store := sessstore.NewStaticStore(mem)
	cmd := rtloop.AgentCommand([]agentkit.Agent{
		stubDetailAgent{id: "default"}, stubDetailAgent{id: "global"},
		stubDetailAgent{id: "channel"}, stubDetailAgent{id: "session"},
	}, store, "default", rtworkspace.Static(t.TempDir()))
	commands, err := sessstore.NewCommands(sessstore.CommandsConfig{}, sessstore.CommandsDeps{SessionStore: store})
	if err != nil {
		t.Fatal(err)
	}
	for _, step := range []struct {
		args string
		want string
	}{
		{"", "default"},
		{"-g use global", "global"},
		{"use session", "session"},
		{"-c use channel", "session"},
		{"-c reset", "session"},
		{"-c use channel", "session"},
		{"reset", "channel"},
		{"use session", "session"},
		{"/new", "channel"},
		{"-c reset", "global"},
		{"-g reset", "default"},
	} {
		t.Run(step.args+"->"+step.want, func(t *testing.T) {
			if step.args == "/new" {
				if _, err := commands.Commands()[0].CommandExec(ctx, ""); err != nil {
					t.Fatal(err)
				}
			} else if _, err := cmd.CommandExec(ctx, step.args); err != nil {
				t.Fatal(err)
			}
			out, err := cmd.CommandExec(ctx, "")
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(out, "Active agent: "+step.want+"\n") {
				t.Fatalf("want active agent %q; output:\n%s", step.want, out)
			}
		})
	}
}

// Regression: under a thread-scoped conversation, /agent -c must bind at the
// channel so the default set in one thread is visible from another thread of
// the same chat. Before the fix the key was thread-scoped and cross-thread
// reads silently fell back to the loop default.
func TestAgentChannelUseAppliesAcrossThreads(t *testing.T) {
	t.Parallel()

	ws, err := rtworkspace.New(rtworkspace.Config{Global: t.TempDir(), Local: t.TempDir(), Scope: "local"})
	if err != nil {
		t.Fatal(err)
	}
	mem, err := sessstore.NewMemory(sessstore.MemoryConfig{ID: "lark:oc_d95"})
	if err != nil {
		t.Fatal(err)
	}
	store := sessstore.NewStaticStore(mem)
	cmd := rtloop.AgentCommand([]agentkit.Agent{
		stubDetailAgent{id: "assistant"}, stubDetailAgent{id: "worker"},
	}, store, "assistant", ws)

	threadCtx := func(topic string) context.Context {
		delivery := rctx.BuildDeliverySessionID("lark", "oc_d95", topic, "ou_user")
		env := rctx.ResolveEnvelope(rctx.WithDeliveryRoute(agentkit.MessageEvent{
			PlatformID: "lark", UserID: "ou_user",
		}, rctx.SessionRoute("lark", string(delivery))), rctx.DefaultRoutePolicy(agentkit.SessionScopeThread))
		env = rctx.WithMetadataScope(env, agentkit.SessionScopeThread)
		return rctx.ApplyEnvelopeToContext(context.Background(), env)
	}

	// Bind in thread A.
	if out, err := cmd.CommandExec(threadCtx("om_topicA"), "-c use worker"); err != nil {
		t.Fatal(err)
	} else if !strings.Contains(out, "channel agent: worker") {
		t.Fatalf("bind out = %q", out)
	}

	// Read back from thread B: must resolve to worker, not the loop default.
	out, err := cmd.CommandExec(threadCtx("om_topicB"), "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Active agent: worker\n") {
		t.Fatalf("cross-thread read did not see channel bind; output:\n%s", out)
	}
}
