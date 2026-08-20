package agent_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/agent"
	"github.com/lengzhao/agentkit/runtime/llm"
	"github.com/lengzhao/agentkit/runtime/prompt"
	"github.com/lengzhao/agentkit/runtime/session"
	"github.com/lengzhao/agentkit/runtime/tools"
)

func TestRunStepStreamsMessageUpdateDeltas(t *testing.T) {
	t.Parallel()

	scripted, err := llm.NewScripted(llm.ScriptedConfig{
		Steps: []llm.ScriptedStep{{Text: "hello world"}},
	})
	if err != nil {
		t.Fatal(err)
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
	rt, err := agent.New(agent.Config{ID: "test", Model: "scripted"}, agent.Deps{
		LLM:     scripted,
		Session: mem,
		Tools:   toolRuntime,
		Prompt:  assembler,
	})
	if err != nil {
		t.Fatal(err)
	}

	var events []agentkit.OutboundEvent
	emit := func(_ context.Context, ev agentkit.OutboundEvent) error {
		events = append(events, ev)
		return nil
	}

	_, err = rt.RunTurn(context.Background(), agentkit.TurnInput{
		Message: agentkit.ModelMessage{
			Role:    "user",
			Content: []agentkit.ContentPart{{Type: "text", Text: "hi"}},
		},
		Emit: emit,
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(events) == 0 {
		t.Fatal("expected streaming outbound events")
	}
	if events[0].Type != agentkit.EventMessageStart {
		t.Fatalf("expected message/start first, got %q", events[0].Type)
	}

	var deltas []string
	var sawEnd bool
	for _, ev := range events {
		switch ev.Type {
		case agentkit.EventMessageUpdate:
			var payload agentkit.MessageUpdatePayload
			if err := json.Unmarshal(ev.Data, &payload); err != nil {
				t.Fatal(err)
			}
			if payload.AssistantMessageEvent.Type == agentkit.AssistantEventTextDelta {
				deltas = append(deltas, payload.AssistantMessageEvent.Delta)
			}
		case agentkit.EventMessageEnd:
			sawEnd = true
		}
	}
	if len(deltas) == 0 {
		t.Fatal("expected text_delta message_update events")
	}
	if got := stringConcat(deltas); got != "hello world" {
		t.Fatalf("unexpected streamed text: %q", got)
	}
	if !sawEnd {
		t.Fatal("expected message/end")
	}
}

func stringConcat(parts []string) string {
	var b []byte
	for _, p := range parts {
		b = append(b, p...)
	}
	return string(b)
}
