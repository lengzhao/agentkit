package agent_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/agent"
	"github.com/lengzhao/agentkit/runtime/llm"
	"github.com/lengzhao/agentkit/runtime/prompt"
	"github.com/lengzhao/agentkit/runtime/rctx"
	"github.com/lengzhao/agentkit/runtime/session/derive"
	sessstore "github.com/lengzhao/agentkit/runtime/session/sessstore"
	"github.com/lengzhao/agentkit/runtime/tools"
	rtworkspace "github.com/lengzhao/agentkit/runtime/workspace"
)

type errAfterTextStream struct {
	sent bool
}

func (s *errAfterTextStream) Name() string { return "err-after-text" }

func (s *errAfterTextStream) Stream(context.Context, agentkit.LLMRequest) (agentkit.LLMStream, error) {
	return s, nil
}

func (s *errAfterTextStream) Recv() (agentkit.LLMEvent, error) {
	if !s.sent {
		s.sent = true
		msg := agentkit.ModelMessage{
			Role:    "assistant",
			Content: []agentkit.ContentPart{{Type: "text", Text: "partial"}},
		}
		return agentkit.LLMEvent{
			Type:         agentkit.AssistantEventTextDelta,
			ContentIndex: 0,
			Delta:        "partial",
			Message:      &msg,
		}, nil
	}
	return agentkit.LLMEvent{}, errors.New("provider failed")
}

func (s *errAfterTextStream) Close() error { return nil }

func TestAssistantMessagePersistsStopReasonOnStreamError(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store, err := sessstore.NewStore(sessstore.StoreConfig{Dir: "."}, sessstore.StoreDeps{Workspace: rtworkspace.Static(dir)})
	if err != nil {
		t.Fatal(err)
	}
	toolRT, err := tools.NewRuntime(tools.RuntimeConfig{}, tools.RuntimeDeps{})
	if err != nil {
		t.Fatal(err)
	}
	assembler, err := prompt.NewAssembler(prompt.AssemblerConfig{}, prompt.AssemblerDeps{})
	if err != nil {
		t.Fatal(err)
	}
	ag, err := agent.New(agent.Config{ID: "test", Model: "m"}, agent.Deps{
		LLM:          &errAfterTextStream{},
		Tools:        toolRT,
		Prompt:       assembler,
		SessionStore: store,
		Workspace:    rtworkspace.Static(dir),
	})
	if err != nil {
		t.Fatal(err)
	}

	sessionID := agentkit.SessionID("persist-stop")
	ctx := rctx.ApplyEnvelopeToContext(context.Background(), agentkit.TurnEnvelope{
		Conversation: string(sessionID),
		Workspace:    string(sessionID),
		AgentID:      agentkit.AgentID("test"),
	})

	err = ag.RunTurn(ctx, agentkit.TurnInput{
		Message: agentkit.ModelMessage{
			Role:    "user",
			Content: []agentkit.ContentPart{{Type: "text", Text: "go"}},
		},
	})
	if err == nil {
		t.Fatal("expected stream error")
	}

	sess, err := store.Get(ctx, sessionID)
	if err != nil {
		t.Fatal(err)
	}
	events, err := derive.ReadAllEvents(ctx, sess)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, ev := range events {
		if ev.Type != agentkit.EventAssistantMessage {
			continue
		}
		var msg agentkit.ModelMessage
		if err := json.Unmarshal(ev.Data, &msg); err != nil {
			t.Fatal(err)
		}
		if msg.StopReason != agentkit.AssistantStopReasonError {
			t.Fatalf("stopReason = %q, want error", msg.StopReason)
		}
		found = true
	}
	if !found {
		t.Fatal("expected persisted assistant message")
	}
}

func TestScriptedSuccessOmitsStopReason(t *testing.T) {
	t.Parallel()

	fix := newTurnFixture(t, nil, agent.Config{ID: "test", Model: "m"}, []llm.ScriptedStep{{Text: "ok"}})
	if err := fix.run(t); err != nil {
		t.Fatal(err)
	}
	events := fix.sessionEvents(t)
	for _, ev := range events {
		if ev.Type != agentkit.EventAssistantMessage {
			continue
		}
		var msg agentkit.ModelMessage
		if err := json.Unmarshal(ev.Data, &msg); err != nil {
			t.Fatal(err)
		}
		if msg.StopReason != "" {
			t.Fatalf("successful assistant should omit stopReason, got %q", msg.StopReason)
		}
		return
	}
	t.Fatal("missing assistant message")
}
