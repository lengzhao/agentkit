package acpremote

import (
	"context"
	"encoding/json"
	"errors"
	capsession "github.com/lengzhao/agentkit/cap/session"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lengzhao/agentkit"
	capacp "github.com/lengzhao/agentkit/cap/acp"
	"github.com/lengzhao/agentkit/runtime/platform/common"
	"github.com/lengzhao/agentkit/runtime/rctx"
	"github.com/lengzhao/agentkit/testing/agenttest"
)

type commandMCPIdentityProbe struct {
	turnMCPProbe
	setupAgent, turnAgent agentkit.AgentID
}

func (p *commandMCPIdentityProbe) SessionMCPServers(ctx context.Context) ([]capacp.SessionMCPServer, error) {
	p.setupAgent = rctx.AgentIDFromContext(ctx)
	return p.turnMCPProbe.SessionMCPServers(ctx)
}

func (p *commandMCPIdentityProbe) BeginMCPTurn(ctx context.Context) (context.Context, func() error, error) {
	p.turnAgent = rctx.AgentIDFromContext(ctx)
	return p.turnMCPProbe.BeginMCPTurn(ctx)
}

func TestACPCommandAndTurnUseSameMCPIdentity(t *testing.T) {
	a, cmd := configTestRuntime(t, "remote", "replay")
	a.cfg.Env["AGENTKIT_CONFIG_PROMPTS"] = filepath.Join(t.TempDir(), "prompts.jsonl")
	p := &commandMCPIdentityProbe{}
	original := a.pool.newBridgeFn
	a.pool.newBridgeFn = func() *bridge { b := original(); b.sessionMCP = p; return b }
	ctx := common.SlashCommandContext(context.Background(), nil, common.SlashContext{
		Route: rctx.SessionRoute("lark", "lark:C1"), UserID: "U1",
	})
	if _, err := cmd.CommandExec(ctx, "model"); err != nil {
		t.Fatal(err)
	}
	if err := a.RunTurn(rctx.WithAgentID(ctx, a.id), agentkit.TurnInput{
		Message: agentkit.ModelMessage{Role: "user", Content: []agentkit.ContentPart{{Type: "text", Text: "hello"}}},
		Emit:    func(context.Context, agentkit.OutboundEvent) error { return nil },
	}); err != nil {
		t.Fatal(err)
	}
	if p.setupAgent != a.id || p.turnAgent != p.setupAgent || p.lists.Load() != 1 {
		t.Fatalf("MCP identity changed on reused connection: setup=%q turn=%q setups=%d", p.setupAgent, p.turnAgent, p.lists.Load())
	}
}

type turnMCPProbe struct {
	begins, ends, lists, releases atomic.Int32
	failure                       error
	beginFailure                  error
}

func (p *turnMCPProbe) SessionMCPServers(context.Context) ([]capacp.SessionMCPServer, error) {
	p.lists.Add(1)
	return nil, nil
}
func (p *turnMCPProbe) BeginMCPTurn(ctx context.Context) (context.Context, func() error, error) {
	p.begins.Add(1)
	if p.beginFailure != nil {
		return nil, nil, p.beginFailure
	}
	turn, cancel := context.WithCancel(ctx)
	return turn, func() error { p.ends.Add(1); cancel(); return p.failure }, nil
}
func (p *turnMCPProbe) ReleaseMCPSession(context.Context) { p.releases.Add(1) }
func TestSessionMCPLifecycleOnReusedConnection(t *testing.T) {
	a, _ := configTestRuntime(t, "test", "replay")
	a.cfg.Env["AGENTKIT_CONFIG_PROMPTS"] = filepath.Join(t.TempDir(), "prompts.jsonl")
	p := &turnMCPProbe{}
	original := a.pool.newBridgeFn
	a.pool.newBridgeFn = func() *bridge { b := original(); b.sessionMCP = p; return b }
	ctx, cancel := context.WithTimeout(agenttest.TurnContext("mcp-turn", "test"), 10*time.Second)
	defer cancel()
	input := agentkit.TurnInput{Message: agentkit.ModelMessage{Role: "user", Content: []agentkit.ContentPart{{Type: "text", Text: "hello"}}}, Emit: func(_ context.Context, event agentkit.OutboundEvent) error {
		if event.Type == agentkit.EventTurnEnd {
			var data capsession.TurnEndData
			if err := json.Unmarshal(event.Data, &data); err != nil {
				return err
			}
			if data.Cancelled {
				t.Error("successful MCP turn marked canceled")
			}
		}
		return nil
	}}
	for i := 0; i < 2; i++ {
		if err := a.RunTurn(ctx, input); err != nil {
			t.Fatal(err)
		}
	}
	if p.begins.Load() != 2 || p.ends.Load() != 2 || p.lists.Load() != 1 {
		t.Fatalf("begin=%d end=%d setup=%d", p.begins.Load(), p.ends.Load(), p.lists.Load())
	}
	p.failure = errors.New("fatal MCP tool error")
	if err := a.RunTurn(ctx, input); !errors.Is(err, p.failure) {
		t.Fatalf("lost tool abort: %v", err)
	}
	a.pool.Close()
	deadline := time.After(2 * time.Second)
	for p.releases.Load() == 0 {
		select {
		case <-deadline:
			t.Fatal("session resources not released")
		default:
			time.Sleep(time.Millisecond)
		}
	}
	if p.releases.Load() != 1 {
		t.Fatalf("release=%d", p.releases.Load())
	}
}

func TestSessionMCPBeginFailurePreservesContextAndCleanup(t *testing.T) {
	a, _ := configTestRuntime(t, "test", "replay")
	failure := errors.New("runtime discovery unavailable")
	p := &turnMCPProbe{beginFailure: failure}
	original := a.pool.newBridgeFn
	a.pool.newBridgeFn = func() *bridge { b := original(); b.sessionMCP = p; return b }
	ctx := agenttest.TurnContext("begin-failure", "test")
	input := agentkit.TurnInput{Message: agentkit.ModelMessage{Role: "user", Content: []agentkit.ContentPart{{Type: "text", Text: "hello"}}}, Emit: func(context.Context, agentkit.OutboundEvent) error { return nil }}
	if err := a.RunTurn(ctx, input); !errors.Is(err, failure) {
		t.Fatalf("begin error lost: %v", err)
	}
	b := requireBridge(t, a, "begin-failure")
	a.pool.closeBridge(b)
	if p.releases.Load() != 1 {
		t.Fatal("failed Begin leaked session resources")
	}
}
