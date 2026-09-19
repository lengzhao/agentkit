package compaction

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/lengzhao/agentkit"
	capcompaction "github.com/lengzhao/agentkit/cap/compaction"
	"github.com/lengzhao/agentkit/runtime/session/derive"
	"github.com/lengzhao/agentkit/runtime/session/sessevents"
	sessstore "github.com/lengzhao/agentkit/runtime/session/sessstore"
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
	svc, err := NewSummary(SummaryConfig{
		MinMessages:      2,
		KeepRecentTokens: 1,
		Retry: &capcompaction.RetryConfig{
			Enabled:     &enabled,
			MaxRetries:  3,
			BaseDelayMs: 1,
		},
	}, SummaryDeps{LLM: llm})
	if err != nil {
		t.Fatal(err)
	}

	mem, err := sessstore.NewMemory(sessstore.MemoryConfig{ID: "summary-retry"})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		if err := sessevents.AppendMessage(ctx, mem, "coder", agentkit.EventUserMessage, agentkit.ModelMessage{
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

	result, err := svc.Compact(ctx, capcompaction.Request{
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

	events, err := derive.ReadAllEvents(ctx, mem)
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

// TestForcedCompactionBelowKeepRecent covers /compact and overflow recovery on a
// short history: Force skips the minMessages gate, so the slice bounds must hold.
func TestForcedCompactionBelowKeepRecent(t *testing.T) {
	t.Parallel()

	svc, err := NewSummary(SummaryConfig{KeepRecentTokens: 20000}, SummaryDeps{LLM: &flakySummaryLLM{}})
	if err != nil {
		t.Fatal(err)
	}
	sess, err := sessstore.NewMemory(sessstore.MemoryConfig{ID: "test:shorthistory"})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := sessevents.AppendMessage(ctx, sess, "a", agentkit.EventUserMessage, agentkit.ModelMessage{
		Role:    "user",
		Content: []agentkit.ContentPart{{Type: "text", Text: "hi"}},
	}); err != nil {
		t.Fatal(err)
	}
	messages, err := sess.DeriveMessages(ctx)
	if err != nil {
		t.Fatal(err)
	}

	result, err := svc.Compact(ctx, capcompaction.Request{
		SessionID: sess.ID(),
		Session:   sess,
		Messages:  messages,
		Force:     true,
	})
	if err != nil {
		t.Fatalf("forced compaction: %v", err)
	}
	if result.Applied {
		t.Fatal("nothing should be compacted when the history fits in keepRecentTokens")
	}
}

type okSummaryLLM struct {
	calls atomic.Int32
}

func (o *okSummaryLLM) Name() string { return "ok-summary" }

func (o *okSummaryLLM) Stream(_ context.Context, _ agentkit.LLMRequest) (agentkit.LLMStream, error) {
	o.calls.Add(1)
	return &summaryStream{text: "summary"}, nil
}

func lastCompactionData(t *testing.T, ctx context.Context, sess agentkit.Session) capcompaction.EventData {
	t.Helper()
	events, err := derive.ReadAllEvents(ctx, sess)
	if err != nil {
		t.Fatal(err)
	}
	for i := len(events) - 1; i >= 0; i-- {
		if events[i].Type != agentkit.EventCompaction {
			continue
		}
		var data capcompaction.EventData
		if err := json.Unmarshal(events[i].Data, &data); err != nil {
			t.Fatal(err)
		}
		return data
	}
	t.Fatal("no session/compaction event")
	return capcompaction.EventData{}
}

// A retained message larger than keepRecentTokens must be truncated in the
// compaction event, otherwise the post-compaction history stays oversized and
// the retry hits context overflow again.
func TestSummaryTruncatesOversizedRetainedMessage(t *testing.T) {
	t.Parallel()

	llm := &okSummaryLLM{}
	svc, err := NewSummary(SummaryConfig{KeepRecentTokens: 200}, SummaryDeps{LLM: llm})
	if err != nil {
		t.Fatal(err)
	}
	sess, err := sessstore.NewMemory(sessstore.MemoryConfig{ID: "test:giantretained"})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for _, msg := range []agentkit.ModelMessage{
		{Role: "user", Content: []agentkit.ContentPart{{Type: "text", Text: "old question"}}},
		{Role: "assistant", Content: []agentkit.ContentPart{{Type: "text", Text: "old answer"}}},
		{Role: "user", Content: []agentkit.ContentPart{{Type: "text", Text: strings.Repeat("x", 5000)}}},
	} {
		if err := sessevents.AppendMessage(ctx, sess, "a", agentkit.EventUserMessage, msg); err != nil {
			t.Fatal(err)
		}
	}
	messages, err := sess.DeriveMessages(ctx)
	if err != nil {
		t.Fatal(err)
	}

	result, err := svc.Compact(ctx, capcompaction.Request{
		SessionID: sess.ID(),
		AgentID:   "a",
		Session:   sess,
		Messages:  messages,
		Force:     true,
	})
	if err != nil {
		t.Fatalf("compact: %v", err)
	}
	if !result.Applied {
		t.Fatal("expected compaction applied")
	}
	data := lastCompactionData(t, ctx, sess)
	if len(data.RetainedTail) == 0 {
		t.Fatal("expected retained tail")
	}
	last := data.RetainedTail[len(data.RetainedTail)-1]
	got := last.Content[0].Text
	if len(got) >= 5000 {
		t.Fatalf("oversized retained message not truncated, len=%d", len(got))
	}
	if !strings.Contains(got, "truncated") {
		t.Fatal("truncated retained message should carry a marker")
	}
}

// When the whole history is one oversized message there is nothing to
// summarize; forced compaction must still bound the history by truncating the
// giant message instead of silently reporting not-applied.
func TestSummaryForceTruncatesWhenNothingToSummarize(t *testing.T) {
	t.Parallel()

	llm := &okSummaryLLM{}
	svc, err := NewSummary(SummaryConfig{KeepRecentTokens: 200}, SummaryDeps{LLM: llm})
	if err != nil {
		t.Fatal(err)
	}
	sess, err := sessstore.NewMemory(sessstore.MemoryConfig{ID: "test:onlygiant"})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := sessevents.AppendMessage(ctx, sess, "a", agentkit.EventUserMessage, agentkit.ModelMessage{
		Role:    "user",
		Content: []agentkit.ContentPart{{Type: "text", Text: strings.Repeat("y", 5000)}},
	}); err != nil {
		t.Fatal(err)
	}
	messages, err := sess.DeriveMessages(ctx)
	if err != nil {
		t.Fatal(err)
	}

	result, err := svc.Compact(ctx, capcompaction.Request{
		SessionID: sess.ID(),
		AgentID:   "a",
		Session:   sess,
		Messages:  messages,
		Force:     true,
	})
	if err != nil {
		t.Fatalf("compact: %v", err)
	}
	if !result.Applied {
		t.Fatal("forced compaction must apply when an oversized message is truncatable")
	}
	if llm.calls.Load() != 0 {
		t.Fatalf("summary llm calls = %d, want 0: nothing to summarize", llm.calls.Load())
	}
	data := lastCompactionData(t, ctx, sess)
	if len(data.RetainedTail) != 1 {
		t.Fatalf("retained tail = %d, want 1", len(data.RetainedTail))
	}
	got := data.RetainedTail[0].Content[0].Text
	if len(got) >= 5000 {
		t.Fatalf("oversized message not truncated, len=%d", len(got))
	}

	derived, err := sess.DeriveMessages(ctx)
	if err != nil {
		t.Fatal(err)
	}
	total := 0
	for _, msg := range derived {
		for _, part := range msg.Content {
			total += len(part.Text)
		}
	}
	if total >= 5000 {
		t.Fatalf("derived history still oversized, chars=%d", total)
	}
}

// 线上事故回归（trace e3ac4ade）：用户发过的大文件/图片在存储时被 sanitize 成
// attachment_ref（几十字节），真实大小只留在事件 metadata 的 logical_chars 里。
// 压缩切点必须按 metadata 的大小累加，否则 Prepare 永远返回 nil、Force 压缩静默
// not applied，会话在 "compaction did not apply" 下永久卡死。
func TestSummaryForceAppliesWhenGiantsSanitizedToAttachments(t *testing.T) {
	t.Parallel()

	llm := &okSummaryLLM{}
	svc, err := NewSummary(SummaryConfig{KeepRecentTokens: 20000}, SummaryDeps{LLM: llm})
	if err != nil {
		t.Fatal(err)
	}
	sess, err := sessstore.NewMemory(sessstore.MemoryConfig{ID: "test:sanitized-giants"})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	giant := strings.Repeat("x", 300_000)
	// 两条旧的巨型附件消息：入站带大文本，存储时剥成 attachment_ref。
	for _, src := range []string{"upload/a.log", "upload/b.log"} {
		if err := sessevents.AppendMessage(ctx, sess, "assistant", agentkit.EventUserMessage, agentkit.ModelMessage{
			Role: "user",
			Content: []agentkit.ContentPart{
				{Type: "document", Text: giant, Source: src, MIME: "text/plain"},
			},
		}); err != nil {
			t.Fatal(err)
		}
	}
	// 近期小消息。
	for i := 0; i < 4; i++ {
		if err := sessevents.AppendMessage(ctx, sess, "assistant", agentkit.EventUserMessage, agentkit.ModelMessage{
			Role:    "user",
			Content: []agentkit.ContentPart{{Type: "text", Text: "继续"}},
		}); err != nil {
			t.Fatal(err)
		}
	}
	messages, err := sess.DeriveMessages(ctx)
	if err != nil {
		t.Fatal(err)
	}

	result, err := svc.Compact(ctx, capcompaction.Request{
		SessionID: sess.ID(),
		AgentID:   "assistant",
		Session:   sess,
		Messages:  messages,
		Force:     true,
	})
	if err != nil {
		t.Fatalf("compact: %v", err)
	}
	if !result.Applied {
		t.Fatal("forced compaction must apply: recorded logical sizes make the giants visible to the cut point")
	}
	if llm.calls.Load() != 1 {
		t.Fatalf("summary llm calls = %d, want 1", llm.calls.Load())
	}
	data := lastCompactionData(t, ctx, sess)
	for _, msg := range data.RetainedTail {
		for _, part := range msg.Content {
			if part.Type == "attachment_ref" {
				t.Fatalf("retained tail must not keep hydratable attachment parts: %#v", part)
			}
		}
	}
}

// 退化形态：整段历史只有一条巨型附件消息 + 一句跟进。切点只能落在 0、
// 没有可摘要内容时，truncateOnly 兜底必须中和附件 part 并落压缩事件，
// 而不是静默 not applied。
func TestSummaryForceNeutralizesSingleGiantAttachment(t *testing.T) {
	t.Parallel()

	llm := &okSummaryLLM{}
	svc, err := NewSummary(SummaryConfig{KeepRecentTokens: 20000}, SummaryDeps{LLM: llm})
	if err != nil {
		t.Fatal(err)
	}
	sess, err := sessstore.NewMemory(sessstore.MemoryConfig{ID: "test:single-giant-attachment"})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := sessevents.AppendMessage(ctx, sess, "assistant", agentkit.EventUserMessage, agentkit.ModelMessage{
		Role: "user",
		Content: []agentkit.ContentPart{
			{Type: "document", Text: strings.Repeat("x", 300_000), Source: "upload/huge.log", MIME: "text/plain"},
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := sessevents.AppendMessage(ctx, sess, "assistant", agentkit.EventUserMessage, agentkit.ModelMessage{
		Role:    "user",
		Content: []agentkit.ContentPart{{Type: "text", Text: "继续"}},
	}); err != nil {
		t.Fatal(err)
	}
	messages, err := sess.DeriveMessages(ctx)
	if err != nil {
		t.Fatal(err)
	}

	result, err := svc.Compact(ctx, capcompaction.Request{
		SessionID: sess.ID(),
		AgentID:   "assistant",
		Session:   sess,
		Messages:  messages,
		Force:     true,
	})
	if err != nil {
		t.Fatalf("compact: %v", err)
	}
	if !result.Applied {
		t.Fatal("forced compaction must apply by neutralizing the oversized attachment")
	}
	if llm.calls.Load() != 0 {
		t.Fatalf("summary llm calls = %d, want 0: nothing to summarize", llm.calls.Load())
	}
	data := lastCompactionData(t, ctx, sess)
	for _, msg := range data.RetainedTail {
		for _, part := range msg.Content {
			if part.Type == "attachment_ref" {
				t.Fatalf("attachment part should have been neutralized: %#v", part)
			}
		}
	}
}
