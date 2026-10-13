package agent_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"sync/atomic"
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

// emptyThenTextLLM returns an empty assistant message on the first call, then
// a real text reply, simulating a transient empty completion from the gateway.
type emptyThenTextLLM struct {
	calls atomic.Int32
	text  string
}

func (e *emptyThenTextLLM) Name() string { return "empty-then-text" }

func (e *emptyThenTextLLM) Stream(_ context.Context, _ agentkit.LLMRequest) (agentkit.LLMStream, error) {
	call := e.calls.Add(1)
	if call == 1 {
		return &emptyStream{}, nil
	}
	return &instantStream{msg: agentkit.ModelMessage{
		Role:    "assistant",
		Content: []agentkit.ContentPart{{Type: "text", Text: e.text}},
	}}, nil
}

// emptyStream emits a single LLMEventMessage with an empty assistant message,
// then EOF — exactly what an upstream "empty completion" looks like.
type emptyStream struct {
	sent bool
}

func (s *emptyStream) Recv() (agentkit.LLMEvent, error) {
	if s.sent {
		return agentkit.LLMEvent{}, io.EOF
	}
	s.sent = true
	msg := agentkit.ModelMessage{Role: "assistant"}
	return agentkit.LLMEvent{Type: agentkit.LLMEventMessage, Message: &msg}, nil
}

func (s *emptyStream) Close() error { return nil }

func buildEmptyTestAgent(t *testing.T, llmProvider agentkit.LLMProvider, retryCfg *agent.RetryConfig) (agentkit.Agent, agentkit.SessionStore, agentkit.SessionID) {
	t.Helper()
	dir := t.TempDir()
	store, err := sessstore.NewStore(sessstore.StoreConfig{Dir: "."}, sessstore.StoreDeps{Workspace: rtworkspace.Static(dir)})
	if err != nil {
		t.Fatal(err)
	}
	assembler, err := prompt.NewAssembler(prompt.AssemblerConfig{}, prompt.AssemblerDeps{})
	if err != nil {
		t.Fatal(err)
	}
	toolRT, err := tools.NewRuntime(tools.RuntimeConfig{}, tools.RuntimeDeps{})
	if err != nil {
		t.Fatal(err)
	}
	ag, err := agent.New(agent.Config{
		ID:    "test",
		Model: "m",
		Retry: retryCfg,
	}, agent.Deps{
		SessionStore: store,
		LLM:          llmProvider,
		Tools:        toolRT,
		Prompt:       assembler,
		Workspace:    rtworkspace.Static(dir),
	})
	if err != nil {
		t.Fatal(err)
	}
	return ag, store, agentkit.SessionID("test:empty")
}

func runEmptyTestTurn(t *testing.T, ag agentkit.Agent, sessionID agentkit.SessionID) error {
	t.Helper()
	ctx := rctx.ApplyEnvelopeToContext(context.Background(), agentkit.TurnEnvelope{
		Conversation: string(sessionID),
		Workspace:    string(sessionID),
	})
	return ag.RunTurn(ctx, agentkit.TurnInput{
		Message: agentkit.ModelMessage{
			Role:    "user",
			Content: []agentkit.ContentPart{{Type: "text", Text: "go"}},
		},
	})
}

func TestEmptyCompletionRetriesAndSucceeds(t *testing.T) {
	t.Parallel()

	provider := &emptyThenTextLLM{text: "recovered reply"}
	enabled := true
	ag, store, sessionID := buildEmptyTestAgent(t, provider, &agent.RetryConfig{
		Enabled: &enabled, MaxRetries: 3, BaseDelayMs: 1,
	})

	if err := runEmptyTestTurn(t, ag, sessionID); err != nil {
		t.Fatalf("run turn: %v", err)
	}
	if provider.calls.Load() != 2 {
		t.Fatalf("expected 2 llm calls, got %d", provider.calls.Load())
	}

	sess, err := store.Get(context.Background(), sessionID)
	if err != nil {
		t.Fatal(err)
	}
	events, err := derive.ReadAllEvents(context.Background(), sess)
	if err != nil {
		t.Fatal(err)
	}

	var retryStarts, retryEnds, emptyAssistants int
	for _, ev := range events {
		switch ev.Type {
		case agentkit.EventAutoRetryStart:
			retryStarts++
		case agentkit.EventAutoRetryEnd:
			retryEnds++
		case agentkit.EventAssistantMessage:
			var msg agentkit.ModelMessage
			if json.Unmarshal(ev.Data, &msg) == nil && msg.StopReason == agentkit.AssistantStopReasonEmpty {
				emptyAssistants++
			}
		}
	}
	if retryStarts != 1 || retryEnds != 1 {
		t.Fatalf("retry events start=%d end=%d, want 1/1", retryStarts, retryEnds)
	}
	// The empty assistant is NOT persisted as a normal message because
	// persistAssistantWithStopReason skips messages with no content/tool-calls.
	// The audit trail is via the "assistant step empty completion" WARN log.
	_ = emptyAssistants
}

func TestEmptyCompletionExhaustsRetries(t *testing.T) {
	t.Parallel()

	provider := &alwaysEmptyLLM{}
	enabled := true
	ag, _, sessionID := buildEmptyTestAgent(t, provider, &agent.RetryConfig{
		Enabled: &enabled, MaxRetries: 2, BaseDelayMs: 1,
	})

	err := runEmptyTestTurn(t, ag, sessionID)
	if err == nil {
		t.Fatal("expected error after exhausting retries")
	}
	if !errors.Is(err, llm.ErrEmptyCompletion) {
		t.Fatalf("expected ErrEmptyCompletion, got: %v", err)
	}
	// 1 initial + 2 retries = 3 total calls
	if provider.calls.Load() != 3 {
		t.Fatalf("expected 3 llm calls, got %d", provider.calls.Load())
	}
}

type alwaysEmptyLLM struct {
	calls atomic.Int32
}

func (a *alwaysEmptyLLM) Name() string { return "always-empty" }

func (a *alwaysEmptyLLM) Stream(_ context.Context, _ agentkit.LLMRequest) (agentkit.LLMStream, error) {
	a.calls.Add(1)
	return &emptyStream{}, nil
}

func TestEmptyCompletionZeroOutputTokensDetected(t *testing.T) {
	t.Parallel()

	provider := &zeroTokenLLM{}
	enabled := true
	ag, _, sessionID := buildEmptyTestAgent(t, provider, &agent.RetryConfig{
		Enabled: &enabled, MaxRetries: 1, BaseDelayMs: 1,
	})

	err := runEmptyTestTurn(t, ag, sessionID)
	if err == nil {
		t.Fatal("expected error for zero-token empty response")
	}
	if !errors.Is(err, llm.ErrEmptyCompletion) {
		t.Fatalf("expected ErrEmptyCompletion, got: %v", err)
	}
	// 1 initial + 1 retry = 2 total
	if provider.calls.Load() != 2 {
		t.Fatalf("expected 2 llm calls, got %d", provider.calls.Load())
	}
}

// zeroTokenLLM returns an assistant with some text content but usage.OutputTokens == 0,
// mimicking a gateway that drops the completion body.
type zeroTokenLLM struct {
	calls atomic.Int32
}

func (z *zeroTokenLLM) Name() string { return "zero-token" }

func (z *zeroTokenLLM) Stream(_ context.Context, _ agentkit.LLMRequest) (agentkit.LLMStream, error) {
	z.calls.Add(1)
	return &zeroTokenStream{}, nil
}

type zeroTokenStream struct {
	sent bool
}

func (s *zeroTokenStream) Recv() (agentkit.LLMEvent, error) {
	if s.sent {
		return agentkit.LLMEvent{}, io.EOF
	}
	s.sent = true
	msg := agentkit.ModelMessage{
		Role:    "assistant",
		Content: []agentkit.ContentPart{{Type: "text", Text: " "}},
	}
	return agentkit.LLMEvent{
		Type:    agentkit.LLMEventMessage,
		Message: &msg,
		Usage:   &agentkit.Usage{InputTokens: 5000, OutputTokens: 0, TotalTokens: 5000},
	}, nil
}

func (s *zeroTokenStream) Close() error { return nil }
