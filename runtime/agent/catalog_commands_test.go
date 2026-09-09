package agent_test

import (
	"context"
	"strings"
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/agent"
)

type stubCatalogAgent struct {
	id agentkit.AgentID
}

func (a stubCatalogAgent) ID() agentkit.AgentID { return a.id }
func (stubCatalogAgent) RunTurn(context.Context, agentkit.TurnInput) error { return nil }

type stubCatalogLoop struct {
	agents       []agentkit.Agent
	defaultAgent agentkit.AgentID
}

func (l *stubCatalogLoop) Dispatch(context.Context, agentkit.LoopRequest) error { return nil }
func (l *stubCatalogLoop) Steer(context.Context, agentkit.ModelMessage) error   { return nil }
func (l *stubCatalogLoop) FollowUp(context.Context, agentkit.ModelMessage) error  { return nil }
func (l *stubCatalogLoop) Cancel(context.Context, string) error                 { return nil }
func (l *stubCatalogLoop) IsSessionBusy(agentkit.SessionID) bool                { return false }
func (l *stubCatalogLoop) TryDeliverPermission(agentkit.MessageEvent) bool        { return false }
func (l *stubCatalogLoop) SupersedePendingForInbound(agentkit.MessageEvent)       {}

func (l *stubCatalogLoop) Agents() []agentkit.Agent               { return l.agents }
func (l *stubCatalogLoop) DefaultAgentID() agentkit.AgentID       { return l.defaultAgent }

func TestCatalogCommandsListAgents(t *testing.T) {
	t.Parallel()

	loop := &stubCatalogLoop{
		agents:       []agentkit.Agent{stubCatalogAgent{id: "assistant"}, stubCatalogAgent{id: "worker"}},
		defaultAgent: "assistant",
	}
	provider, err := agent.NewCatalogCommands(agent.CatalogCommandsConfig{}, agent.CatalogCommandsDeps{
		Loop: loop,
	})
	if err != nil {
		t.Fatal(err)
	}
	var agentCmd agentkit.Command
	for _, cmd := range provider.Commands() {
		if cmd.Name() == "agent" {
			agentCmd = cmd
			break
		}
	}
	if agentCmd == nil {
		t.Fatal("missing /agent command")
	}
	out, err := agentCmd.CommandExec(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "assistant") || !strings.Contains(out, "worker") {
		t.Fatalf("out = %q, want registered agents listed", out)
	}
}
