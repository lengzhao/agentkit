package agent_test

import (
	"context"
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/workspace"
	_ "github.com/lengzhao/agentkit/plugins"
	"github.com/lengzhao/agentkit/runtime/session"
	"github.com/lengzhao/pluginkit/build"
)

func TestRunTurnWritesLifecycleEventsInOrder(t *testing.T) {
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
							map[string]any{
								"text": "",
								"toolCalls": []any{
									map[string]any{
										"id":    "call-1",
										"name":  "read",
										"input": `{"path":"README.md"}`,
									},
								},
							},
							map[string]any{
								"text": "done",
							},
						},
					},
				},
				"prompt": map[string]any{"use": "prompt/assembler/default"},
				"tools": map[string]any{
					"use": "tools/runtime",
					"deps": map[string]any{
						"tools": []any{
							map[string]any{
								"use": "tool/read-file",
								"deps": map[string]any{
									"fs": map[string]any{
										"use": "fs/memory",
										"config": map[string]any{
											"files": map[string]string{"README.md": "hello"},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	ag, _, err := build.Build[agentkit.Agent](context.Background(), graph, "agent")
	if err != nil {
		t.Fatalf("build agent: %v", err)
	}

	ctx := context.WithValue(context.Background(), agentkit.KeySessionID, sessionID)
	if err := ag.RunTurn(ctx, agentkit.TurnInput{
		Message: agentkit.ModelMessage{
			Role:    "user",
			Content: []agentkit.ContentPart{{Type: "text", Text: "read README"}},
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
	events, err := session.ReadAllEvents(ctx, sess)
	if err != nil {
		t.Fatalf("read events: %v", err)
	}

	got := eventTypes(events)
	want := []agentkit.EventType{
		agentkit.EventTurnStart,
		agentkit.EventUserMessage,
		agentkit.EventStepStart,
		agentkit.EventAssistantMessage,
		agentkit.EventToolCall,
		agentkit.EventToolResult,
		agentkit.EventStepEnd,
		agentkit.EventStepStart,
		agentkit.EventAssistantMessage,
		agentkit.EventStepEnd,
		agentkit.EventTurnEnd,
	}
	if len(got) != len(want) {
		t.Fatalf("event count = %d, want %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("event[%d] = %q, want %q\nall: %v", i, got[i], want[i], got)
		}
	}

	beforeSeq := session.LatestEventSeq(events[:len(events)-1])
	if beforeSeq == 0 {
		t.Fatal("expected non-zero seq before turn/end")
	}
	if events[len(events)-1].Type != agentkit.EventTurnEnd {
		t.Fatalf("last event = %q, want turn/end", events[len(events)-1].Type)
	}
	if events[len(events)-1].Seq <= beforeSeq {
		t.Fatalf("turn/end seq %d should be after prior events", events[len(events)-1].Seq)
	}
}

func eventTypes(events []agentkit.SessionEvent) []agentkit.EventType {
	out := make([]agentkit.EventType, len(events))
	for i, ev := range events {
		out[i] = ev.Type
	}
	return out
}
