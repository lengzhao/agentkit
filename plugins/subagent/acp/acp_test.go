package acp

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/lengzhao/agentkit"
	capsubagent "github.com/lengzhao/agentkit/cap/subagent"
	"github.com/lengzhao/agentkit/plugins/agent/acpremote"
	"github.com/lengzhao/agentkit/runtime/rctx"
	"github.com/lengzhao/agentkit/runtime/session/sessevents"
	"github.com/lengzhao/agentkit/runtime/session/sessstore"
	rtworkspace "github.com/lengzhao/agentkit/runtime/workspace"
)

type child struct {
	id       agentkit.AgentID
	store    agentkit.SessionStore
	contexts []context.Context
	stopped  int
	wait     bool
}

func (c *child) ID() agentkit.AgentID    { return c.id }
func (c *child) ShutdownHooks() []func() { return []func(){func() { c.stopped++ }} }
func (c *child) RunTurn(ctx context.Context, in agentkit.TurnInput) error {
	c.contexts = append(c.contexts, ctx)
	if c.wait {
		<-ctx.Done()
		return ctx.Err()
	}
	if in.Emit == nil {
		return errors.New("missing emit")
	}
	s, err := c.store.Get(ctx, rctx.SessionIDFromContext(ctx))
	if err != nil {
		return err
	}
	return sessevents.Default.AppendMessage(ctx, s, c.id, agentkit.EventAssistantMessage, agentkit.ModelMessage{Role: "assistant", Content: []agentkit.ContentPart{{Type: "text", Text: "answer: " + in.Message.Content[0].Text}}})
}
func config() Config {
	return Config{Agents: []Entry{{Name: "reviewer", Description: "Review code", ACP: acpremote.Config{Command: []string{"unused"}}}}}
}
func parent() context.Context {
	ctx := rctx.ApplyEnvelopeToContext(context.Background(), agentkit.TurnEnvelope{Workspace: "tenant", Conversation: "parent", AgentID: "main", Actor: agentkit.ActorRef{UserID: "user"}, Route: rctx.SessionRoute("cli", "parent")})
	return rctx.ContextWithOutboundEmit(ctx, func(context.Context, agentkit.OutboundEvent) error { return nil })
}
func setup(t *testing.T) (Deps, *child) {
	t.Helper()
	ws := rtworkspace.Static(t.TempDir())
	store, err := sessstore.NewStore(sessstore.StoreConfig{Dir: "."}, sessstore.StoreDeps{Workspace: ws})
	if err != nil {
		t.Fatal(err)
	}
	return Deps{Workspace: ws, SessionStore: store}, &child{store: store}
}
func TestDelegationIsolatedWithoutMCP(t *testing.T) {
	deps, c := setup(t)
	s, err := newWithFactory(config(), deps, func(cfg acpremote.Config, _ acpremote.Deps) (agentkit.Agent, error) {
		if cfg.AutoApprove {
			t.Fatal("auto approval")
		}
		c.id = cfg.ID
		return c, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	defs, err := s.Definitions(parent())
	if err != nil || len(defs) != 1 || defs[0].Name != "reviewer" {
		t.Fatalf("catalog: %+v %v", defs, err)
	}
	for i := 0; i < 2; i++ {
		out, err := s.Run(parent(), capsubagent.Request{Agent: "reviewer", Task: "check"})
		if err != nil || out.Summary != "answer: check" || out.Session == "parent" {
			t.Fatalf("run: %+v %v", out, err)
		}
	}
	if rctx.SessionIDFromContext(c.contexts[0]) == rctx.SessionIDFromContext(c.contexts[1]) {
		t.Fatal("child history reused")
	}
	env := rctx.EnvelopeFromContext(c.contexts[0])
	if env.Workspace != "tenant" || env.Actor.UserID != "user" {
		t.Fatalf("identity lost: %+v", env)
	}
	for _, stop := range s.ShutdownHooks() {
		stop()
		stop()
	}
	if c.stopped != 1 {
		t.Fatalf("shutdown %d", c.stopped)
	}
	if _, err = s.Run(parent(), capsubagent.Request{Agent: "reviewer", Task: "check"}); err == nil {
		t.Fatal("run after shutdown")
	}
}
func TestRejectsAsyncAndCancellation(t *testing.T) {
	deps, c := setup(t)
	c.wait = true
	s, err := newWithFactory(config(), deps, func(cfg acpremote.Config, _ acpremote.Deps) (agentkit.Agent, error) { c.id = cfg.ID; return c, nil })
	if err != nil {
		t.Fatal(err)
	}
	yes := true
	if _, err = s.Run(parent(), capsubagent.Request{Agent: "reviewer", Task: "check", Async: &yes}); err == nil {
		t.Fatal("async accepted")
	}
	ctx, cancel := context.WithCancel(parent())
	cancel()
	if _, err = s.Run(ctx, capsubagent.Request{Agent: "reviewer", Task: "check"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel: %v", err)
	}
}
func TestConfigValidation(t *testing.T) {
	cases := []Config{{}, {TimeoutSeconds: -1}, {Agents: []Entry{{Name: "r", Description: "d"}}}, {Agents: []Entry{{Name: "r", Description: "d", ACP: acpremote.Config{Command: []string{"x"}, AutoApprove: true}}}}, {Agents: []Entry{config().Agents[0], config().Agents[0]}}}
	for _, cfg := range cases {
		if err := cfg.Validate(); err == nil {
			t.Fatalf("accepted %+v", cfg)
		}
	}
	deps, c := setup(t)
	cfg := config()
	cfg.Agents = append(cfg.Agents, Entry{Name: "writer", Description: "write", ACP: acpremote.Config{Command: []string{"x"}}})
	n := 0
	_, err := newWithFactory(cfg, deps, func(cfg acpremote.Config, _ acpremote.Deps) (agentkit.Agent, error) {
		n++
		if n == 2 {
			return nil, errors.New("construct failed")
		}
		c.id = cfg.ID
		return c, nil
	})
	if err == nil || !strings.Contains(err.Error(), "construct failed") || c.stopped != 1 {
		t.Fatalf("cleanup: %v %d", err, c.stopped)
	}
}
