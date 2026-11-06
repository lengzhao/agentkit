package acp

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/permission"
	capsubagent "github.com/lengzhao/agentkit/cap/subagent"
	"github.com/lengzhao/agentkit/plugins/agent/acpremote"
	"github.com/lengzhao/agentkit/runtime/filesystem"
	"github.com/lengzhao/agentkit/runtime/rctx"
	rttelemetry "github.com/lengzhao/agentkit/runtime/telemetry"
)

// A real stdio peer exercises the ACP client, history persistence and lifecycle
// without a network service, model credentials or MCP endpoint.
func TestACPSubprocessPeer(t *testing.T) {
	if os.Getenv("AGENTKIT_ACP_SUBAGENT_TEST") != "1" {
		return
	}
	dec := json.NewDecoder(os.Stdin)
	enc := json.NewEncoder(os.Stdout)
	for {
		var req struct {
			ID     any             `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if dec.Decode(&req) != nil {
			os.Exit(0)
		}
		if req.ID == nil {
			continue
		}
		var result any
		switch req.Method {
		case "initialize":
			result = map[string]any{"protocolVersion": 1, "agentCapabilities": map[string]any{}, "authMethods": []any{}}
		case "session/new":
			var params struct {
				MCPServers []any `json:"mcpServers"`
			}
			json.Unmarshal(req.Params, &params)
			if len(params.MCPServers) != 0 {
				os.Exit(3)
			}
			result = map[string]any{"sessionId": "remote"}
		case "session/prompt":
			enc.Encode(map[string]any{"jsonrpc": "2.0", "method": "session/update", "params": map[string]any{"sessionId": "remote", "update": map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]any{"type": "text", "text": "ACP answer"}}}})
			result = map[string]any{"stopReason": "end_turn"}
		default:
			result = map[string]any{}
		}
		enc.Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result})
	}
}

type approvalParent struct{ env agentkit.TurnEnvelope }

func (p *approvalParent) PermissionCapability() permission.Capability {
	return permission.Capability{Interactive: true}
}
func (p *approvalParent) Await(ctx context.Context, _ permission.Request) (permission.Result, error) {
	p.env = rctx.EnvelopeFromContext(ctx)
	return permission.Result{Outcome: permission.OutcomeNoHuman}, nil
}

type approvalChild struct{ id agentkit.AgentID }

func (c approvalChild) ID() agentkit.AgentID { return c.id }
func (c approvalChild) RunTurn(ctx context.Context, _ agentkit.TurnInput) error {
	broker, ok := permission.BrokerFrom(ctx)
	if !ok {
		return errors.New("missing parent broker")
	}
	if !permission.CapabilityFrom(ctx).Interactive {
		return errors.New("lost parent capability")
	}
	_, err := broker.Await(ctx, permission.Request{})
	return err
}
func TestApprovalReturnsToParentInteraction(t *testing.T) {
	deps, _ := setup(t)
	s, err := newWithFactory(config(), deps, func(cfg acpremote.Config, _ acpremote.Deps) (agentkit.Agent, error) {
		return approvalChild{id: cfg.ID}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	defer s.stop()
	broker := &approvalParent{}
	ctx := context.WithValue(parent(), agentkit.KeySessionControl, broker)
	if _, err = s.Run(ctx, capsubagent.Request{Agent: "reviewer", Task: "ask"}); err != nil {
		t.Fatal(err)
	}
	if broker.env.Conversation != "parent" || broker.env.AgentID != "reviewer" || broker.env.Actor.UserID != "user" {
		t.Fatalf("approval misrouted: %+v", broker.env)
	}
}
func TestConcurrentDelegationsHaveDifferentHistories(t *testing.T) {
	deps, _ := setup(t)
	c := &blockingChild{started: make(chan context.Context, 2)}
	s, err := newWithFactory(config(), deps, func(cfg acpremote.Config, _ acpremote.Deps) (agentkit.Agent, error) { c.id = cfg.ID; return c, nil })
	if err != nil {
		t.Fatal(err)
	}
	defer s.stop()
	ctx, cancel := context.WithCancel(parent())
	defer cancel()
	done := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() { _, err := s.Run(ctx, capsubagent.Request{Agent: "reviewer", Task: "wait"}); done <- err }()
	}
	ids := map[agentkit.SessionID]bool{}
	for i := 0; i < 2; i++ {
		select {
		case child := <-c.started:
			ids[rctx.SessionIDFromContext(child)] = true
		case <-time.After(3 * time.Second):
			t.Fatal("child did not start")
		}
	}
	if len(ids) != 2 {
		t.Fatalf("shared histories: %v", ids)
	}
	cancel()
	for i := 0; i < 2; i++ {
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Fatal("child did not stop")
		}
	}
}
func TestRealACPDelegationWithoutMCP(t *testing.T) {
	deps, _ := setup(t)
	var err error
	deps.FS, err = filesystem.New(filesystem.Config{}, filesystem.Deps{Workspace: deps.Workspace})
	if err != nil {
		t.Fatal(err)
	}
	deps.Telemetry, err = rttelemetry.NewToolkit(struct{}{}, struct{}{})
	if err != nil {
		t.Fatal(err)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cfg := config()
	cfg.Agents[0].ACP = acpremote.Config{Command: []string{exe, "-test.run=^TestACPSubprocessPeer$"}, Env: map[string]string{"AGENTKIT_ACP_SUBAGENT_TEST": "1"}}
	s, err := New(cfg, deps)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for _, h := range s.ShutdownHooks() {
			h()
		}
	})
	ctx, cancel := context.WithTimeout(parent(), 10*time.Second)
	defer cancel()
	out, err := s.Run(ctx, capsubagent.Request{Agent: "reviewer", Task: "answer"})
	if err != nil || out.Summary != "ACP answer" {
		t.Fatalf("ACP: %+v %v", out, err)
	}
}

type blockingChild struct {
	id      agentkit.AgentID
	started chan context.Context
}

func (c *blockingChild) ID() agentkit.AgentID { return c.id }
func (c *blockingChild) RunTurn(ctx context.Context, _ agentkit.TurnInput) error {
	c.started <- ctx
	<-ctx.Done()
	return ctx.Err()
}
func TestRunningChildCancellationTimeoutAndShutdown(t *testing.T) {
	for _, mode := range []string{"parent", "timeout", "shutdown"} {
		t.Run(mode, func(t *testing.T) {
			deps, _ := setup(t)
			c := &blockingChild{started: make(chan context.Context, 1)}
			s, err := newWithFactory(config(), deps, func(cfg acpremote.Config, _ acpremote.Deps) (agentkit.Agent, error) { c.id = cfg.ID; return c, nil })
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(parent())
			defer cancel()
			req := capsubagent.Request{Agent: "reviewer", Task: "wait"}
			if mode == "timeout" {
				seconds := 1
				req.TimeoutSeconds = &seconds
			}
			done := make(chan error, 1)
			go func() { _, err := s.Run(ctx, req); done <- err }()
			select {
			case <-c.started:
			case <-time.After(3 * time.Second):
				t.Fatal("child did not start")
			}
			switch mode {
			case "parent":
				cancel()
			case "shutdown":
				s.ShutdownHooks()[0]()
			}
			select {
			case err := <-done:
				want := context.Canceled
				if mode == "timeout" {
					want = context.DeadlineExceeded
				}
				if !errors.Is(err, want) {
					t.Fatalf("got %v want %v", err, want)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("child did not stop")
			}
			s.ShutdownHooks()[0]()
		})
	}
}
