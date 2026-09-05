package agent_test

import (
	"context"
	"testing"
	"time"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/workspace"
	_ "github.com/lengzhao/agentkit/plugins"
	"github.com/lengzhao/agentkit/runtime/agent"
	"github.com/lengzhao/agentkit/runtime/loop"
	"github.com/lengzhao/agentkit/runtime/prompt"
	"github.com/lengzhao/agentkit/runtime/session"
	"github.com/lengzhao/agentkit/runtime/tools"
	"github.com/lengzhao/pluginkit/build"
)

func TestSteerInjectsBeforeNextStep(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	sessionID := agentkit.SessionID("test:default")
	graph := map[string]any{
		"agent": map[string]any{
			"use": "agent/coding",
			"config": map[string]any{
				"id":       "test",
				"maxSteps": 5,
			},
			"deps": map[string]any{
				"sessionStore": map[string]any{
					"use":    "session/store",
					"config": map[string]any{"dir": "."},
					"deps": map[string]any{
						"workspace": map[string]any{
							"use":    "workspace/default",
							"config": map[string]any{"root": dir},
						},
					},
				},
				"llm": map[string]any{
					"use": "llm/scripted",
					"config": map[string]any{
						"steps": []any{
							map[string]any{"text": "first"},
							map[string]any{"text": "second"},
						},
					},
				},
				"prompt": map[string]any{"use": "prompt/assembler/default"},
				"workspace": map[string]any{
					"use":    "workspace/default",
					"config": map[string]any{"root": dir},
				},
				"tools": map[string]any{
					"use": "tools/runtime",
					"deps": map[string]any{
						"tools": []any{},
					},
				},
			},
		},
	}

	ag, _, err := build.Build[agentkit.Agent](context.Background(), graph, "agent")
	if err != nil {
		t.Fatalf("build agent: %v", err)
	}

	ctrl := loop.NewControl()
	ctx := session.ApplyEnvelopeToContext(context.Background(), agentkit.TurnEnvelope{Conversation: string(sessionID), Workspace: string(sessionID)})
	ctx = context.WithValue(ctx, agentkit.KeySessionControl, ctrl)
	if err := ctrl.Steer(ctx, agentkit.ModelMessage{
		Role:    "user",
		Content: []agentkit.ContentPart{{Type: "text", Text: "steered"}},
	}); err != nil {
		t.Fatal(err)
	}

	if err := ag.RunTurn(ctx, agentkit.TurnInput{
		Message: agentkit.ModelMessage{
			Role:    "user",
			Content: []agentkit.ContentPart{{Type: "text", Text: "start"}},
		},
	}); err != nil {
		t.Fatalf("run turn: %v", err)
	}

	store, err := session.NewStore(session.StoreConfig{Dir: "."}, session.StoreDeps{Workspace: workspace.Static(dir)})
	if err != nil {
		t.Fatal(err)
	}
	sess, err := store.Get(ctx, sessionID)
	if err != nil {
		t.Fatal(err)
	}
	msgs, err := sess.DeriveMessages(ctx)
	if err != nil {
		t.Fatal(err)
	}
	foundStart := false
	foundSteer := false
	for _, msg := range msgs {
		if len(msg.Content) == 0 {
			continue
		}
		switch msg.Content[0].Text {
		case "start":
			foundStart = true
		case "steered":
			foundSteer = true
		}
	}
	if !foundStart || !foundSteer {
		t.Fatalf("expected start and steered messages, got %+v", msgs)
	}
}

func TestSteerResetsSegmentMaxSteps(t *testing.T) {
	t.Parallel()

	block := &blockingLLM{
		release: make(chan struct{}),
		started: make(chan struct{}),
	}
	assembler, err := prompt.NewAssembler(prompt.AssemblerConfig{}, prompt.AssemblerDeps{})
	if err != nil {
		t.Fatal(err)
	}
	mem, err := session.NewMemory(session.MemoryConfig{ID: "s1"})
	if err != nil {
		t.Fatal(err)
	}
	toolRuntime, err := tools.NewRuntime(tools.RuntimeConfig{}, tools.RuntimeDeps{})
	if err != nil {
		t.Fatal(err)
	}
	rt, err := agent.New(agent.Config{ID: "test", Model: "blocking", MaxSteps: 1}, agent.Deps{
		SessionStore: session.NewStaticStore(mem),
		LLM:          block,
		Tools:        toolRuntime,
		Prompt:       assembler,
		Workspace:    workspace.Static(t.TempDir()),
	})
	if err != nil {
		t.Fatal(err)
	}

	ctrl := loop.NewControl()
	ctx := session.ApplyEnvelopeToContext(context.Background(), agentkit.TurnEnvelope{Conversation: string(mem.ID()), Workspace: string(mem.ID())})
	ctx = context.WithValue(ctx, agentkit.KeySessionControl, ctrl)
	turnDone := make(chan error, 1)
	go func() {
		turnDone <- rt.RunTurn(ctx, agentkit.TurnInput{
			Message: agentkit.ModelMessage{
				Role:    "user",
				Content: []agentkit.ContentPart{{Type: "text", Text: "start"}},
			},
		})
	}()

	select {
	case <-block.started:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for LLM stream to start")
	}

	if err := ctrl.Steer(ctx, agentkit.ModelMessage{
		Role:    "user",
		Content: []agentkit.ContentPart{{Type: "text", Text: "steered after budget"}},
	}); err != nil {
		t.Fatal(err)
	}
	close(block.release)

	select {
	case err := <-turnDone:
		if err != nil {
			t.Fatalf("run turn: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for turn to finish")
	}

	if block.calls < 2 {
		t.Fatalf("llm calls = %d, want at least 2 after steer reset", block.calls)
	}

	msgs, err := mem.DeriveMessages(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var texts []string
	for _, msg := range msgs {
		if msg.Role == "user" && len(msg.Content) > 0 {
			texts = append(texts, msg.Content[0].Text)
		}
	}
	if len(texts) < 2 || texts[1] != "steered after budget" {
		t.Fatalf("user messages = %v", texts)
	}
}
