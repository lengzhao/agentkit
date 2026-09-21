package compaction

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/lengzhao/agentkit"
	capcompaction "github.com/lengzhao/agentkit/cap/compaction"
	"github.com/lengzhao/agentkit/runtime/session/derive"
	"github.com/lengzhao/agentkit/runtime/session/sessevents"
	sessstore "github.com/lengzhao/agentkit/runtime/session/sessstore"
)

func testSummaryDeps(t *testing.T, llm agentkit.LLMProvider) SummaryDeps {
	t.Helper()
	events, err := sessevents.New()
	if err != nil {
		t.Fatal(err)
	}
	return SummaryDeps{LLM: llm, SessionEvents: events}
}

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
	}, testSummaryDeps(t, llm))
	if err != nil {
		t.Fatal(err)
	}

	mem, err := sessstore.NewMemory(sessstore.MemoryConfig{ID: "summary-retry"})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		if err := sessevents.Default.AppendMessage(ctx, mem, "coder", agentkit.EventUserMessage, agentkit.ModelMessage{
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

	svc, err := NewSummary(SummaryConfig{KeepRecentTokens: 20000}, testSummaryDeps(t, &flakySummaryLLM{}))
	if err != nil {
		t.Fatal(err)
	}
	sess, err := sessstore.NewMemory(sessstore.MemoryConfig{ID: "test:shorthistory"})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := sessevents.Default.AppendMessage(ctx, sess, "a", agentkit.EventUserMessage, agentkit.ModelMessage{
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
	svc, err := NewSummary(SummaryConfig{KeepRecentTokens: 200}, testSummaryDeps(t, llm))
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
		if err := sessevents.Default.AppendMessage(ctx, sess, "a", agentkit.EventUserMessage, msg); err != nil {
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
	svc, err := NewSummary(SummaryConfig{KeepRecentTokens: 200}, testSummaryDeps(t, llm))
	if err != nil {
		t.Fatal(err)
	}
	sess, err := sessstore.NewMemory(sessstore.MemoryConfig{ID: "test:onlygiant"})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := sessevents.Default.AppendMessage(ctx, sess, "a", agentkit.EventUserMessage, agentkit.ModelMessage{
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
	svc, err := NewSummary(SummaryConfig{KeepRecentTokens: 20000}, testSummaryDeps(t, llm))
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
		if err := sessevents.Default.AppendMessage(ctx, sess, "assistant", agentkit.EventUserMessage, agentkit.ModelMessage{
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
		if err := sessevents.Default.AppendMessage(ctx, sess, "assistant", agentkit.EventUserMessage, agentkit.ModelMessage{
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
	svc, err := NewSummary(SummaryConfig{KeepRecentTokens: 20000}, testSummaryDeps(t, llm))
	if err != nil {
		t.Fatal(err)
	}
	sess, err := sessstore.NewMemory(sessstore.MemoryConfig{ID: "test:single-giant-attachment"})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := sessevents.Default.AppendMessage(ctx, sess, "assistant", agentkit.EventUserMessage, agentkit.ModelMessage{
		Role: "user",
		Content: []agentkit.ContentPart{
			{Type: "document", Text: strings.Repeat("x", 300_000), Source: "upload/huge.log", MIME: "text/plain"},
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := sessevents.Default.AppendMessage(ctx, sess, "assistant", agentkit.EventUserMessage, agentkit.ModelMessage{
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

// 摘要流与 openai accumulator 一致：每个 text_delta 同时带累计 Message 快照和
// 增量 Delta，结束再发一篇完整 message。只应保留最终正文，不能把快照拼进去。
func TestSummaryDoesNotAccumulateSnapshotPlusDelta(t *testing.T) {
	t.Parallel()

	llm := &snapshotDeltaLLM{chunks: []string{"Hel", "lo", " world"}}
	svc, err := NewSummary(SummaryConfig{KeepRecentTokens: 1}, testSummaryDeps(t, llm))
	if err != nil {
		t.Fatal(err)
	}
	sess, err := sessstore.NewMemory(sessstore.MemoryConfig{ID: "test:snapshot-delta-summary"})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for i := 0; i < 4; i++ {
		if err := sessevents.Default.AppendMessage(ctx, sess, "a", agentkit.EventUserMessage, agentkit.ModelMessage{
			Role:    "user",
			Content: []agentkit.ContentPart{{Type: "text", Text: fmt.Sprintf("old-%d %s", i, strings.Repeat("z", 80))}},
		}); err != nil {
			t.Fatal(err)
		}
	}
	messages, err := sess.DeriveMessages(ctx)
	if err != nil {
		t.Fatal(err)
	}
	result, err := svc.Compact(ctx, capcompaction.Request{
		SessionID: sess.ID(), AgentID: "a", Session: sess, Messages: messages, Force: true,
	})
	if err != nil {
		t.Fatalf("compact: %v", err)
	}
	if !result.Applied {
		t.Fatal("expected compaction applied")
	}
	got := lastCompactionData(t, ctx, sess).Summary.Content[0].Text
	if got != "Hello world" {
		t.Fatalf("summary = %q, want exact stream text without snapshot duplication", got)
	}
}

type snapshotDeltaLLM struct {
	chunks []string
	calls  atomic.Int32
}

func (s *snapshotDeltaLLM) Name() string { return "snapshot-delta" }

func (s *snapshotDeltaLLM) Stream(_ context.Context, _ agentkit.LLMRequest) (agentkit.LLMStream, error) {
	s.calls.Add(1)
	return &snapshotDeltaStream{chunks: s.chunks}, nil
}

type snapshotDeltaStream struct {
	chunks []string
	acc    string
	i      int
	final  bool
}

func (s *snapshotDeltaStream) Recv() (agentkit.LLMEvent, error) {
	if s.i < len(s.chunks) {
		s.acc += s.chunks[s.i]
		delta := s.chunks[s.i]
		s.i++
		msg := agentkit.ModelMessage{Role: "assistant", Content: []agentkit.ContentPart{{Type: "text", Text: s.acc}}}
		return agentkit.LLMEvent{Type: agentkit.AssistantEventTextDelta, Delta: delta, Message: &msg}, nil
	}
	if !s.final {
		s.final = true
		msg := agentkit.ModelMessage{Role: "assistant", Content: []agentkit.ContentPart{{Type: "text", Text: s.acc}}}
		return agentkit.LLMEvent{Type: agentkit.LLMEventMessage, Message: &msg}, nil
	}
	return agentkit.LLMEvent{}, io.EOF
}

func (s *snapshotDeltaStream) Close() error { return nil }

// 已落盘的巨型摘要（上次流式拼接错误留下的）必须在强制压缩时被截断，
// 不能因为 Prepare 从 boundaryStart=1 跳过摘要就静默 not applied。
func TestSummaryForceTruncatesOversizedPreviousSummary(t *testing.T) {
	t.Parallel()

	llm := &okSummaryLLM{}
	svc, err := NewSummary(SummaryConfig{KeepRecentTokens: 200}, testSummaryDeps(t, llm))
	if err != nil {
		t.Fatal(err)
	}
	sess, err := sessstore.NewMemory(sessstore.MemoryConfig{ID: "test:giant-previous-summary"})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := sessevents.Default.AppendMessage(ctx, sess, "assistant", agentkit.EventUserMessage, agentkit.ModelMessage{
		Role:    "user",
		Content: []agentkit.ContentPart{{Type: "text", Text: "old"}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := sessevents.Default.AppendCompaction(ctx, sess, "assistant", capcompaction.EventData{
		FirstKeptSeq: 1,
		Kind:         capcompaction.KindSummary,
		Summary: agentkit.ModelMessage{
			Role:    "user",
			Content: []agentkit.ContentPart{{Type: "text", Text: strings.Repeat("S", 300_000)}},
		},
		RetainedTail: []agentkit.ModelMessage{{
			Role:    "user",
			Content: []agentkit.ContentPart{{Type: "text", Text: "继续"}},
		}},
	}); err != nil {
		t.Fatal(err)
	}
	messages, err := sess.DeriveMessages(ctx)
	if err != nil {
		t.Fatal(err)
	}
	result, err := svc.Compact(ctx, capcompaction.Request{
		SessionID: sess.ID(), AgentID: "assistant", Session: sess, Messages: messages, Force: true,
	})
	if err != nil {
		t.Fatalf("compact: %v", err)
	}
	if !result.Applied {
		t.Fatal("forced compaction must shrink an oversized previous summary")
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
	if total >= 300_000 {
		t.Fatalf("derived history still carries the giant summary, chars=%d", total)
	}
}

// 线上验证：indexed 存储形态很小（attachment_ref），但 req.Messages 是 hydrate 后的
// 大图。keepRecent 切点看不见这些体积 → Prepare nil；单条也不超 per-message
// 预算 → 旧 truncate-only no-op。Force 必须中和附件并落盘，避免下一轮再 hydrate。
func TestSummaryForceFitsHydratedAttachmentsWhenStoredTiny(t *testing.T) {
	t.Parallel()

	llm := &okSummaryLLM{}
	svc, err := NewSummary(SummaryConfig{KeepRecentTokens: 500}, testSummaryDeps(t, llm))
	if err != nil {
		t.Fatal(err)
	}
	sess, err := sessstore.NewMemory(sessstore.MemoryConfig{ID: "test:tiny-stored-hydrated-send"})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for i := 0; i < 8; i++ {
		if err := sessevents.Default.AppendMessage(ctx, sess, "assistant", agentkit.EventUserMessage, agentkit.ModelMessage{
			Role: "user",
			Content: []agentkit.ContentPart{
				{Type: "attachment_ref", Source: fmt.Sprintf("upload/%d.png", i), MIME: "image/png"},
				{Type: "text", Text: "看图"},
			},
		}); err != nil {
			t.Fatal(err)
		}
	}
	hydrated := make([]agentkit.ModelMessage, 0, 8)
	for i := 0; i < 8; i++ {
		hydrated = append(hydrated, agentkit.ModelMessage{
			Role: "user",
			Content: []agentkit.ContentPart{
				{Type: "image_url", URL: "data:image/png;base64," + strings.Repeat("A", 4000)},
				{Type: "text", Text: "看图"},
			},
		})
	}
	result, err := svc.Compact(ctx, capcompaction.Request{
		SessionID: sess.ID(),
		AgentID:   "assistant",
		Session:   sess,
		Messages:  hydrated,
		Force:     true,
	})
	if err != nil {
		t.Fatalf("compact: %v", err)
	}
	if !result.Applied {
		t.Fatal("forced compaction must apply when hydrated send size exceeds keepRecentTokens")
	}
	if llm.calls.Load() != 0 {
		t.Fatalf("summary llm calls = %d, want 0", llm.calls.Load())
	}
	data := lastCompactionData(t, ctx, sess)
	for _, msg := range data.RetainedTail {
		for _, part := range msg.Content {
			if part.Type == "attachment_ref" || part.Type == "image_url" {
				t.Fatalf("retained tail still hydratable: %#v", part)
			}
		}
	}
}

// 回归：历史上只有一次 legacy 空 tail 压缩、之后零新事件时，强制压缩写出的
// 事件必须保证 RetainedTail 非空。否则 derive 把它当 legacy 标记（AfterSeq=
// BeforeSeq=0），重启后全量读文件会从 seq 0 回放，被压缩隐藏的旧历史会复活。
func TestSummaryForceNeverWritesEmptyRetainedTail(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "session.jsonl")
	sess, err := sessstore.NewJSONL(sessstore.JSONLConfig{Path: path, ID: "test:never-empty-tail"})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	// 一条已被过去压缩隐藏的巨型旧消息
	if err := sessevents.Default.AppendMessage(ctx, sess, "a", agentkit.EventUserMessage, agentkit.ModelMessage{
		Role:    "user",
		Content: []agentkit.ContentPart{{Type: "text", Text: strings.Repeat("G", 5000)}},
	}); err != nil {
		t.Fatal(err)
	}
	// legacy 格式 compaction：无 RetainedTail，BeforeSeq=1，巨型 summary
	if err := sessevents.Default.AppendCompaction(ctx, sess, "a", capcompaction.EventData{
		BeforeSeq: 1,
		Kind:      capcompaction.KindSummary,
		Summary: agentkit.ModelMessage{
			Role:    "user",
			Content: []agentkit.ContentPart{{Type: "text", Text: strings.Repeat("S", 100_000)}},
		},
	}); err != nil {
		t.Fatal(err)
	}

	svc, err := NewSummary(SummaryConfig{KeepRecentTokens: 100}, testSummaryDeps(t, &okSummaryLLM{}))
	if err != nil {
		t.Fatal(err)
	}
	messages, err := sess.DeriveMessages(ctx)
	if err != nil {
		t.Fatal(err)
	}
	result, err := svc.Compact(ctx, capcompaction.Request{
		SessionID: sess.ID(), AgentID: "a", Session: sess, Messages: messages, Force: true,
	})
	if err != nil {
		t.Fatalf("compact: %v", err)
	}
	if !result.Applied {
		t.Fatal("expected forced compaction to bound the oversized legacy summary")
	}
	if data := lastCompactionData(t, ctx, sess); len(data.RetainedTail) == 0 {
		t.Fatal("compaction event must never persist an empty retained tail")
	}

	// 模拟进程重启：重新打开同一个 JSONL 文件，被压缩隐藏的旧消息不得复活。
	reopened, err := sessstore.NewJSONL(sessstore.JSONLConfig{Path: path, ID: "test:never-empty-tail"})
	if err != nil {
		t.Fatal(err)
	}
	messages, err = reopened.DeriveMessages(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Compact(ctx, capcompaction.Request{
		SessionID: reopened.ID(), AgentID: "a", Session: reopened, Messages: messages, Force: true,
	}); err != nil {
		t.Fatalf("compact after reopen: %v", err)
	}
	derived, err := reopened.DeriveMessages(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, msg := range derived {
		for _, part := range msg.Content {
			if strings.Contains(part.Text, "GGGG") {
				t.Fatalf("compacted-away message resurrected into model view: %.80q", part.Text)
			}
		}
	}
}
