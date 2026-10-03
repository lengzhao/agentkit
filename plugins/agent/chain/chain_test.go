package chain

import (
	"context"
	"errors"
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/llm"
	"github.com/lengzhao/agentkit/runtime/rctx"
	"github.com/lengzhao/agentkit/testing/agenttest"
)

type stubNode struct {
	id     agentkit.AgentID
	err    error
	seen   *[]string
	agents *[]string
}

func (s stubNode) ID() agentkit.AgentID { return s.id }

func (s stubNode) RunTurn(ctx context.Context, input agentkit.TurnInput) error {
	*s.seen = append(*s.seen, agenttest.ContentText(input.Message))
	*s.agents = append(*s.agents, string(rctx.AgentIDFromContext(ctx)))
	return s.err
}

func textMsg(text string) agentkit.ModelMessage {
	return agentkit.ModelMessage{Role: "user", Content: []agentkit.ContentPart{{Type: "text", Text: text}}}
}

func TestChainRunsNodesInOrder(t *testing.T) {
	var seen, agents []string
	a := stubNode{id: "router", seen: &seen, agents: &agents}
	b := stubNode{id: "coding", seen: &seen, agents: &agents}
	chainAg, err := New(Config{ID: "pipeline", Nodes: []agentkit.AgentID{"router", "coding"}}, Deps{Agents: []agentkit.Agent{a, b}})
	if err != nil {
		t.Fatal(err)
	}
	ctx := agenttest.TurnContext("s1", "pipeline")
	if err := chainAg.RunTurn(ctx, agentkit.TurnInput{Message: textMsg("hello")}); err != nil {
		t.Fatal(err)
	}
	if len(seen) != 2 || seen[0] != "hello" || seen[1] != "" {
		t.Fatalf("seen = %v", seen)
	}
	if len(agents) != 2 || agents[0] != "router" || agents[1] != "coding" {
		t.Fatalf("agents = %v", agents)
	}
}

func TestChainAbortsOnErrorByDefault(t *testing.T) {
	var seen, agents []string
	boom := errors.New("boom")
	a := stubNode{id: "router", err: boom, seen: &seen, agents: &agents}
	b := stubNode{id: "coding", seen: &seen, agents: &agents}
	chainAg, err := New(Config{ID: "pipeline", Nodes: []agentkit.AgentID{"router", "coding"}}, Deps{Agents: []agentkit.Agent{a, b}})
	if err != nil {
		t.Fatal(err)
	}
	err = chainAg.RunTurn(agenttest.TurnContext("s1", "pipeline"), agentkit.TurnInput{Message: textMsg("hi")})
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}
	if len(seen) != 1 {
		t.Fatalf("seen = %v", seen)
	}
}

func TestChainContinueOnError(t *testing.T) {
	var seen, agents []string
	a := stubNode{id: "router", err: errors.New("boom"), seen: &seen, agents: &agents}
	b := stubNode{id: "coding", seen: &seen, agents: &agents}
	chainAg, err := New(Config{ID: "pipeline", Nodes: []agentkit.AgentID{"router", "coding"}, ContinueOnError: true}, Deps{Agents: []agentkit.Agent{a, b}})
	if err != nil {
		t.Fatal(err)
	}
	if err := chainAg.RunTurn(agenttest.TurnContext("s1", "pipeline"), agentkit.TurnInput{Message: textMsg("hi")}); err != nil {
		t.Fatal(err)
	}
	if len(seen) != 2 {
		t.Fatalf("seen = %v", seen)
	}
}

func TestChainRejectsUnknownNode(t *testing.T) {
	a := stubNode{id: "router"}
	if _, err := New(Config{ID: "pipeline", Nodes: []agentkit.AgentID{"router", "missing"}}, Deps{Agents: []agentkit.Agent{a}}); err == nil {
		t.Fatal("expected error for unknown node")
	}
	if _, err := New(Config{ID: "pipeline"}, Deps{Agents: []agentkit.Agent{a}}); err == nil {
		t.Fatal("expected error for empty nodes")
	}
}

// TestChainWithRealAgents verifies the inbound user message is recorded once
// on the shared session and both nodes complete a turn.
func TestChainWithRealAgents(t *testing.T) {
	store, _ := agenttest.TempFileStore(t)
	router, _ := agenttest.NewScriptedAgent(t, agenttest.ScriptedAgentConfig{
		AgentID: "router",
		Store:   store,
		Steps:   []llm.ScriptedStep{{Text: "route: coding"}},
	})
	coding, _ := agenttest.NewScriptedAgent(t, agenttest.ScriptedAgentConfig{
		AgentID: "coding",
		Store:   store,
		Steps:   []llm.ScriptedStep{{Text: "done"}},
	})
	chainAg, err := New(Config{ID: "pipeline", Nodes: []agentkit.AgentID{"router", "coding"}}, Deps{Agents: []agentkit.Agent{router, coding}})
	if err != nil {
		t.Fatal(err)
	}
	agenttest.RunTurn(t, agenttest.TurnContext("s1", "pipeline"), chainAg, "build a thing")

	events := agenttest.SessionEvents(t, context.Background(), store, "s1")
	if got := agenttest.CountEvents(events, agentkit.EventUserMessage); got != 1 {
		t.Fatalf("user messages = %d, want 1", got)
	}
	if got := agenttest.CountEvents(events, agentkit.EventTurnStart); got != 2 {
		t.Fatalf("turn/start = %d, want 2", got)
	}
	if got := agenttest.CountEvents(events, agentkit.EventTurnEnd); got != 2 {
		t.Fatalf("turn/end = %d, want 2", got)
	}
}
