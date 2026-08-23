package compactionsummary_test

import (
	"context"
	"fmt"
	"io"
	"sync/atomic"
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/compaction"
	"github.com/lengzhao/agentkit/plugins/compactionsummary"
	"github.com/lengzhao/agentkit/runtime/session"
)

type flakySummaryLLM struct {
	calls atomic.Int32
}

func (f *flakySummaryLLM) Name() string { return "flaky-summary" }

func (f *flakySummaryLLM) Stream(_ context.Context, _ agentkit.LLMRequest) (agentkit.LLMStream, error) {
	if f.calls.Add(1) == 1 {
		return nil, fmt.Errorf("connection lost")
	}
	return &summaryStream{text: "compressed context"}, nil
}

type summaryStream struct {
	text string
	done bool
}

func (s *summaryStream) Recv() (agentkit.LLMEvent, error) {
	if s.done {
		return agentkit.LLMEvent{}, io.EOF
	}
	s.done = true
	msg := agentkit.ModelMessage{
		Role:    "assistant",
		Content: []agentkit.ContentPart{{Type: "text", Text: s.text}},
	}
	return agentkit.LLMEvent{Type: agentkit.LLMEventMessage, Message: &msg}, nil
}

func (s *summaryStream) Close() error { return nil }

func TestSummaryRetriesTransientLLMError(t *testing.T) {
	t.Parallel()

	llm := &flakySummaryLLM{}
	enabled := true
	svc, err := compactionsummary.New(compactionsummary.Config{
		MinMessages: 2,
		KeepRecent:  1,
		Retry: &compaction.RetryConfig{
			Enabled:     &enabled,
			MaxRetries:  3,
			BaseDelayMs: 1,
		},
	}, compactionsummary.Deps{LLM: llm})
	if err != nil {
		t.Fatal(err)
	}

	mem, err := session.NewMemory(session.MemoryConfig{ID: "summary-retry"})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		if err := session.AppendMessage(ctx, mem, "coder", agentkit.EventUserMessage, agentkit.ModelMessage{
			Role:    "user",
			Content: []agentkit.ContentPart{{Type: "text", Text: fmt.Sprintf("msg %d", i)}},
		}); err != nil {
			t.Fatal(err)
		}
	}
	messages, err := mem.DeriveMessages(ctx)
	if err != nil {
		t.Fatal(err)
	}

	result, err := svc.Compact(ctx, compaction.Request{
		SessionID: mem.ID(),
		AgentID:   "coder",
		Session:   mem,
		Messages:  messages,
		Force:     true,
	})
	if err != nil {
		t.Fatalf("compact: %v", err)
	}
	if !result.Applied {
		t.Fatal("expected compaction applied")
	}
	if llm.calls.Load() != 2 {
		t.Fatalf("expected 2 llm calls, got %d", llm.calls.Load())
	}

	events, err := session.ReadAllEvents(ctx, mem)
	if err != nil {
		t.Fatal(err)
	}
	var retryStarts, retryEnds int
	for _, ev := range events {
		switch ev.Type {
		case agentkit.EventSummarizationRetryStart:
			retryStarts++
		case agentkit.EventSummarizationRetryEnd:
			retryEnds++
		}
	}
	if retryStarts != 1 || retryEnds != 1 {
		t.Fatalf("retry events start=%d end=%d", retryStarts, retryEnds)
	}
}
