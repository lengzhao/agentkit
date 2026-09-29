package feishu

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/lengzhao/agentkit"
	capsession "github.com/lengzhao/agentkit/cap/session"
	"github.com/lengzhao/agentkit/runtime/rctx"
)

func TestRenderProgressMarkdownMergesThinkingAndTool(t *testing.T) {
	p := &Platform{
		showThinking:     true,
		showToolProgress: true,
	}
	st := streamStateLiteral(streamStateData{
		thinking: "plan",
		steps: []toolStep{
			{Kind: toolStepKindTool, Name: "Read", Summary: "README.md", Done: true},
			{Kind: toolStepKindToolResult, Name: "Read", Result: "hello"},
		},
	})
	md := p.renderProgressMarkdown(st, true)
	if strings.Contains(md, "⏱ 运行中") {
		t.Fatalf("streaming progress should not include running status line, got %q", md)
	}
	if !strings.Contains(md, "> plan") {
		t.Fatalf("expected thinking quote block, got %q", md)
	}
	if !strings.Contains(md, "**Read**") {
		t.Fatalf("expected tool line, got %q", md)
	}
	if !strings.Contains(md, "hello") {
		t.Fatalf("expected tool result, got %q", md)
	}
}

func TestRenderProgressMarkdownFinalStatus(t *testing.T) {
	p := &Platform{showToolProgress: true}
	st := streamStateLiteral(streamStateData{
		startedAt:         time.Now().Add(-2 * time.Second),
		progressStartedAt: time.Now().Add(-2 * time.Second),
		steps:             []toolStep{{Kind: toolStepKindTool, Name: "Read", Summary: "a.go"}},
	})
	md := p.renderProgressMarkdown(st, false)
	if !strings.Contains(md, "> ⏱ 用时") {
		t.Fatalf("expected completed status footer, got %q", md)
	}
	if !strings.Contains(md, "---") {
		t.Fatalf("expected status footer separator, got %q", md)
	}
	if !strings.Contains(md, "1 个工具") {
		t.Fatalf("expected tool count in final status, got %q", md)
	}
}

func TestRichCardPatchKeepsSingleProgressHandleAcrossEvents(t *testing.T) {
	p := &Platform{
		showThinking:       true,
		showToolProgress:   true,
		useInteractiveCard: true,
	}
	sessionID := agentkit.SessionID("session-rich-patch")
	st := p.streamState(sessionID)
	st.lock()
	st.thinking = "plan"
	st.bodyText = "hello"
	st.toolStepIdx = make(map[int]int)
	st.startedAt = time.Now()
	st.lastProgressUpdate = time.Now().Add(-time.Second)
	st.unlock()

	ev := agentkit.OutboundEvent{AgentID: "parent"}
	if err := p.handleRichStreamUpdate(context.Background(), sessionID, ev, agentkit.AssistantMessageEvent{
		Type:  agentkit.AssistantEventThinkingDelta,
		Delta: "more",
	}); err != nil {
		t.Fatal(err)
	}
	if err := p.handleRichStreamUpdate(context.Background(), sessionID, ev, agentkit.AssistantMessageEvent{
		Type:         agentkit.AssistantEventToolCallStart,
		ContentIndex: 1,
		ToolName:     "Read",
	}); err != nil {
		t.Fatal(err)
	}

	st.lock()
	defer st.unlock()
	if st.thinking != "planmore" {
		t.Fatalf("thinking should accumulate, got %q", st.thinking)
	}
	if len(st.steps) != 1 || st.steps[0].Name != "Read" {
		t.Fatalf("steps = %#v", st.steps)
	}
}

func TestApplyRichStreamEventToolAndThinking(t *testing.T) {
	p := &Platform{
		showThinking:     true,
		showToolProgress: true,
	}
	st := streamStateLiteral(streamStateData{toolStepIdx: make(map[int]int)})

	if !p.applyRichStreamEvent(st, agentkit.AssistantMessageEvent{
		Type:  agentkit.AssistantEventThinkingDelta,
		Delta: "plan",
	}) {
		t.Fatal("expected thinking delta to update state")
	}
	if st.thinking != "plan" {
		t.Fatalf("thinking = %q", st.thinking)
	}

	if !p.applyRichStreamEvent(st, agentkit.AssistantMessageEvent{
		Type:         agentkit.AssistantEventToolCallStart,
		ContentIndex: 1,
		ToolName:     "Read",
	}) {
		t.Fatal("expected tool start to update state")
	}
	if len(st.steps) != 1 || st.steps[0].Name != "Read" {
		t.Fatalf("steps = %#v", st.steps)
	}

	if !p.applyRichStreamEvent(st, agentkit.AssistantMessageEvent{
		Type:         agentkit.AssistantEventToolCallEnd,
		ContentIndex: 1,
		ToolCall:     &agentkit.ToolCall{Name: "Read", Input: []byte(`{"path":"README.md"}`)},
	}) {
		t.Fatal("expected tool end to update state")
	}
	if !st.steps[0].Done {
		t.Fatalf("step not done: %#v", st.steps[0])
	}
	if st.steps[0].Status != "called" {
		t.Fatalf("call step status = %q, want called", st.steps[0].Status)
	}
	if st.steps[0].Success != nil {
		t.Fatalf("call step should not carry success flag: %#v", st.steps[0])
	}

	if !p.applyToolResult(st, agentkit.ToolResult{
		ID:      "call_1",
		Name:    "Read",
		Content: "hello agentkit",
	}) {
		t.Fatal("expected tool result to update state")
	}
	if len(st.steps) != 2 || st.steps[1].Kind != toolStepKindToolResult {
		t.Fatalf("steps = %#v", st.steps)
	}
	if st.steps[1].Result != "hello agentkit" {
		t.Fatalf("result step = %#v", st.steps[1])
	}

	if p.applyRichStreamEvent(st, agentkit.AssistantMessageEvent{
		Type:  agentkit.AssistantEventTextDelta,
		Delta: "hello",
	}) {
		t.Fatal("text delta should be handled by body lane, not applyRichStreamEvent")
	}
}

func TestRichStreamUpdateIgnoresThinkingWhenDisabled(t *testing.T) {
	p := &Platform{showThinking: false}
	st := streamStateLiteral(streamStateData{})
	if p.applyRichStreamEvent(st, agentkit.AssistantMessageEvent{
		Type:  agentkit.AssistantEventThinkingDelta,
		Delta: "secret",
	}) {
		t.Fatal("should ignore thinking when showThinking is false")
	}
}

func TestRichCardMessageStartPreservesToolSteps(t *testing.T) {
	p := &Platform{useInteractiveCard: true, showToolProgress: true}
	sessionID := agentkit.SessionID("session-unified-steps")
	st := p.streamState(sessionID)
	st.lock()
	st.steps = []toolStep{{Kind: toolStepKindTool, Name: "Read", Summary: "hello.txt", Done: true}}
	st.thinking = "plan"
	st.bodyText = "partial answer"
	st.unlock()

	if err := p.handleRichStreamMessageStart(context.Background(), sessionID); err != nil {
		t.Fatal(err)
	}

	st.lock()
	defer st.unlock()
	if len(st.steps) != 1 || st.steps[0].Name != "Read" {
		t.Fatalf("expected tool steps to persist across messages, got %#v", st.steps)
	}
	if st.thinking != "plan" {
		t.Fatalf("thinking should persist, got %q", st.thinking)
	}
	if st.bodyText != "" {
		t.Fatalf("bodyText should reset for new message, got %q", st.bodyText)
	}
	if st.committedBodyText != "partial answer" {
		t.Fatalf("committedBodyText = %q, want partial answer preserved", st.committedBodyText)
	}
}

func TestRichCardDisplayBodyJoinsCommittedAndInflight(t *testing.T) {
	t.Parallel()
	st := streamStateLiteral(streamStateData{committedBodyText: "first", bodyText: "second"})
	got := st.richCardDisplayBody()
	want := "first" + richCardBodySegmentSeparator + "second"
	if got != want {
		t.Fatalf("display body = %q, want %q", got, want)
	}
}

func TestOutboundStreamKeyUsesReplyTo(t *testing.T) {
	t.Parallel()
	event := agentkit.OutboundEvent{
		Route: rctx.BuildSessionRoute(agentkit.SessionRouteInput{
			Platform:   "feishu",
			DeliveryID: agentkit.SessionID("feishu:oc_chat:u:U1"),
			ReplyTo:    "om_msg_1",
		}),
	}
	got := outboundStreamKey(event)
	want := agentkit.SessionID("feishu:oc_chat:u:U1:reply:om_msg_1")
	if got != want {
		t.Fatalf("stream key = %q, want %q", got, want)
	}
}

func TestEvictStreamCardsDropsOldestProgress(t *testing.T) {
	p := &Platform{useInteractiveCard: false}
	p1 := &feishuPreviewHandle{messageID: "p1"}
	st := streamStateLiteral(streamStateData{
		cards: []streamCard{
			{Kind: streamCardProgress, Handle: p1},
			{Kind: streamCardBody, Handle: &feishuPreviewHandle{messageID: "b1"}},
			{Kind: streamCardProgress, Handle: &feishuPreviewHandle{messageID: "p2"}},
			{Kind: streamCardBody, Handle: &feishuPreviewHandle{messageID: "b2"}},
		},
		progressHandle: p1,
	})
	p.evictStreamCards(context.Background(), st)
	if len(st.cards) != 3 {
		t.Fatalf("cards len = %d, want 3", len(st.cards))
	}
	if st.cards[0].Handle.(*feishuPreviewHandle).messageID != "b1" {
		t.Fatalf("oldest card = %v, want body b1", st.cards[0].Handle)
	}
	if st.cards[2].Handle.(*feishuPreviewHandle).messageID != "b2" {
		t.Fatalf("newest card = %v, want body b2", st.cards[2].Handle)
	}
	if st.progressHandle != nil {
		t.Fatal("evicted progress handle should clear active ref")
	}
}

func TestRemovePriorProgressCardsKeepsLatestOnly(t *testing.T) {
	p := &Platform{useInteractiveCard: false}
	p1 := &feishuPreviewHandle{messageID: "p1"}
	p2 := &feishuPreviewHandle{messageID: "p2"}
	st := streamStateLiteral(streamStateData{
		cards: []streamCard{
			{Kind: streamCardBody, Handle: &feishuPreviewHandle{messageID: "b1"}},
			{Kind: streamCardProgress, Handle: p1},
			{Kind: streamCardBody, Handle: &feishuPreviewHandle{messageID: "b2"}},
		},
		progressHandle: p1,
	})
	p.removePriorProgressCards(context.Background(), st, p2)
	if len(st.cards) != 2 {
		t.Fatalf("cards len = %d, want 2", len(st.cards))
	}
	if st.cards[0].Handle.(*feishuPreviewHandle).messageID != "b1" ||
		st.cards[1].Handle.(*feishuPreviewHandle).messageID != "b2" {
		t.Fatalf("body cards should remain, got %#v", st.cards)
	}
	if st.progressHandle != nil {
		t.Fatal("removed progress handle should clear active ref")
	}
}

func TestEvictStreamCardsPopsBodyWithoutDelete(t *testing.T) {
	p := &Platform{useInteractiveCard: false}
	st := streamStateLiteral(streamStateData{
		cards: []streamCard{
			{Kind: streamCardBody, Handle: &feishuPreviewHandle{messageID: "b1"}},
			{Kind: streamCardProgress, Handle: &feishuPreviewHandle{messageID: "p1"}},
			{Kind: streamCardBody, Handle: &feishuPreviewHandle{messageID: "b2"}},
			{Kind: streamCardProgress, Handle: &feishuPreviewHandle{messageID: "p2"}},
		},
	})
	p.evictStreamCards(context.Background(), st)
	if len(st.cards) != 3 {
		t.Fatalf("cards len = %d, want 3", len(st.cards))
	}
	if st.cards[0].Handle.(*feishuPreviewHandle).messageID != "p1" {
		t.Fatalf("oldest card = %v, want progress p1", st.cards[0].Handle)
	}
	if st.cards[2].Handle.(*feishuPreviewHandle).messageID != "p2" {
		t.Fatalf("newest card = %v, want progress p2", st.cards[2].Handle)
	}
}

func TestStreamFlushDelay(t *testing.T) {
	interval := 800 * time.Millisecond
	if streamFlushDelay(time.Time{}, interval) != 0 {
		t.Fatal("zero last update should flush immediately")
	}
	recent := time.Now()
	if d := streamFlushDelay(recent, interval); d <= 0 || d > interval {
		t.Fatalf("recent update delay = %v, want (0, %v]", d, interval)
	}
	stale := time.Now().Add(-interval)
	if streamFlushDelay(stale, interval) != 0 {
		t.Fatal("stale update should flush immediately")
	}
}

func TestHandleRichTurnEndDoesNotDeadlockWithBodyFlushTimer(t *testing.T) {
	p := &Platform{useInteractiveCard: true}
	sessionID := agentkit.SessionID("session-turn-end-deadlock")
	p.richStreamState(sessionID)
	p.scheduleBodyFlush(sessionID)

	done := make(chan struct{})
	go func() {
		_ = p.handleRichTurnEnd(context.Background(), sessionID, capsession.TurnEndData{Steps: 1})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("handleRichTurnEnd deadlocked while holding stream mutex")
	}
}

func TestAsyncStreamKeyParsing(t *testing.T) {
	parent := agentkit.SessionID("feishu:oc_test:u:ou_1:reply:om_parent")
	asyncKey := asyncStreamKey(parent, "job-1")
	if got := deliveryFromStreamKey(asyncKey); got != agentkit.SessionID("feishu:oc_test:u:ou_1") {
		t.Fatalf("delivery = %q", got)
	}
	if got := replyToFromStreamKey(asyncKey); got != "om_parent" {
		t.Fatalf("replyTo = %q", got)
	}
	// 无 reply 段的父 key 也应正确剥离 async 后缀。
	plain := agentkit.SessionID("feishu:oc_test")
	if got := deliveryFromStreamKey(asyncStreamKey(plain, "j")); got != plain {
		t.Fatalf("delivery = %q", got)
	}
	if got := replyToFromStreamKey(asyncStreamKey(plain, "j")); got != "" {
		t.Fatalf("replyTo = %q", got)
	}
}

func TestScheduleRichCardKeepaliveReschedules(t *testing.T) {
	p := &Platform{}
	sessionID := agentkit.SessionID("session-keepalive")
	st := p.streamState(sessionID)

	// 无卡片时不排心跳。
	p.scheduleRichCardKeepalive(sessionID)
	st.lock()
	if st.keepaliveTimer != nil {
		st.unlock()
		t.Fatal("expected no keepalive timer without progress handle")
	}
	st.progressHandle = &feishuPreviewHandle{messageID: "m1"}
	st.unlock()

	p.scheduleRichCardKeepalive(sessionID)
	st.lock()
	first := st.keepaliveTimer
	st.unlock()
	if first == nil {
		t.Fatal("expected keepalive timer")
	}

	// 再次调用应重置定时器（心跳窗口随成功 flush 后移）。
	p.scheduleRichCardKeepalive(sessionID)
	st.lock()
	second := st.keepaliveTimer
	st.unlock()
	if second == nil || second == first {
		t.Fatal("expected keepalive timer to be rescheduled")
	}

	// clearStream 必须停掉心跳，避免 turn 结束后泄漏。
	p.clearStream(sessionID)
	st.lock()
	if st.keepaliveTimer != nil {
		st.unlock()
		t.Fatal("expected keepalive timer stopped after clearStream")
	}
	st.unlock()
}

func TestOnRichCardKeepaliveAfterStreamCleared(t *testing.T) {
	p := &Platform{}
	sessionID := agentkit.SessionID("session-keepalive-cleared")
	st := p.streamState(sessionID)
	st.lock()
	st.progressHandle = &feishuPreviewHandle{messageID: "m1"}
	st.unlock()
	p.clearStream(sessionID)

	// state 已删除：回调应直接返回，且不得重建 state。
	p.onRichCardKeepalive(sessionID)
	if _, ok := p.streams.Load(sessionID); ok {
		t.Fatal("keepalive callback must not recreate cleared stream state")
	}
}

func TestBodyStreamClosedDisablesElementStream(t *testing.T) {
	p := &Platform{}
	st := p.streamState(agentkit.SessionID("session-stream-closed"))
	handle := &feishuPreviewHandle{messageID: "m1", cardID: "c1"}

	st.lock()
	st.progressHandle = handle
	st.unlock()
	if !p.canRichCardStreamBody(st, handle) {
		t.Fatal("expected body stream allowed on fresh card")
	}

	// 300309 后应标记关闭，后续直接走整卡 patch。
	err := errors.New("lark: stream card content code=300309 msg=ErrMsg: streaming mode is closed; ")
	if !isStreamClosedError(err) {
		t.Fatal("expected stream closed error detected")
	}
	st.lock()
	st.bodyStreamClosed = true
	st.unlock()
	if p.canRichCardStreamBody(st, handle) {
		t.Fatal("expected body stream disabled after 300309")
	}

	if isStreamClosedError(errors.New("code=999999 other")) {
		t.Fatal("unrelated error must not be treated as stream closed")
	}
	if isStreamClosedError(nil) {
		t.Fatal("nil error must not be treated as stream closed")
	}
}

func TestCreateProgressCardBackoff(t *testing.T) {
	// useInteractiveCard=false → SendPreviewStart 返回 errNotSupported，模拟持续创建失败。
	p := &Platform{}
	sessionID := agentkit.SessionID("session-create-backoff")

	// 第一次失败：记录退避窗口。
	if _, err := p.createProgressCard(context.Background(), sessionID, replyContext{}, "{}"); err == nil {
		t.Fatal("expected create error")
	}
	st := p.streamState(sessionID)
	st.lock()
	if st.createFailCount != 1 || st.nextCreateAfter.IsZero() {
		st.unlock()
		t.Fatalf("expected backoff recorded, count=%d after=%v", st.createFailCount, st.nextCreateAfter)
	}
	st.unlock()

	// 退避窗口内不再打 API，直接返回 cooldown。
	if _, err := p.createProgressCard(context.Background(), sessionID, replyContext{}, "{}"); !errors.Is(err, errRichCardCreateCooldown) {
		t.Fatalf("expected cooldown error, got %v", err)
	}
	st.lock()
	if st.createFailCount != 1 {
		st.unlock()
		t.Fatalf("cooldown attempt should not increment fail count, got %d", st.createFailCount)
	}
	st.unlock()

	// 退避窗口过后允许重试（仍失败，退避加深）。
	st.lock()
	st.nextCreateAfter = time.Now().Add(-time.Second)
	st.unlock()
	if _, err := p.createProgressCard(context.Background(), sessionID, replyContext{}, "{}"); err == nil || errors.Is(err, errRichCardCreateCooldown) {
		t.Fatalf("expected real create error after cooldown, got %v", err)
	}
	st.lock()
	if st.createFailCount != 2 {
		st.unlock()
		t.Fatalf("expected fail count 2, got %d", st.createFailCount)
	}
	st.unlock()
}

func TestRecoverRichCardUpdateFailureSwallowsAndClearsHandle(t *testing.T) {
	// useInteractiveCard=false → SendPreviewStart 返回 errNotSupported，模拟重建失败。
	p := &Platform{}
	sessionID := agentkit.SessionID("session-recover")
	st := p.streamState(sessionID)
	st.lock()
	st.progressHandle = &feishuPreviewHandle{messageID: "m1", cardID: "c1"}
	st.unlock()

	p.recoverRichCardUpdateFailure(context.Background(), sessionID, replyContext{}, "{}", context.DeadlineExceeded)

	st.lock()
	if st.progressHandle != nil {
		st.unlock()
		t.Fatal("expected handle cleared after failed recreate")
	}
	st.unlock()

	// 重建失败不限次数：再次失败仍走同一路径（handle 保持为空，下轮 flush 重试创建）。
	p.recoverRichCardUpdateFailure(context.Background(), sessionID, replyContext{}, "{}", context.DeadlineExceeded)
	st.lock()
	if st.progressHandle != nil {
		st.unlock()
		t.Fatal("expected handle still cleared")
	}
	st.unlock()
}

func TestScheduleBodyFlushSetsTimerOnce(t *testing.T) {
	p := &Platform{}
	sessionID := agentkit.SessionID("session-body-timer")
	st := p.streamState(sessionID)
	st.lock()
	st.steps = []toolStep{{Kind: toolStepKindTool, Name: "Read", Summary: "a.go"}}
	st.lastProgressUpdate = time.Now()
	st.unlock()

	p.scheduleBodyFlush(sessionID)
	st.lock()
	first := st.bodyFlushTimer
	st.unlock()
	if first == nil {
		t.Fatal("expected body flush timer")
	}

	p.scheduleBodyFlush(sessionID)
	st.lock()
	second := st.bodyFlushTimer
	st.unlock()
	if first != second {
		t.Fatal("expected pending body flush timer to be reused")
	}
	first.Stop()
}

func TestToolCallEndMatchesByCallIDWithSharedContentIndex(t *testing.T) {
	p := &Platform{showToolProgress: true}
	st := streamStateLiteral(streamStateData{toolStepIdx: make(map[int]int)})

	const sharedIdx = 2
	if !p.applyRichStreamEvent(st, agentkit.AssistantMessageEvent{
		Type:         agentkit.AssistantEventToolCallStart,
		ContentIndex: sharedIdx,
		ID:           "call_a",
		ToolName:     "Shell",
	}) {
		t.Fatal("tool a start")
	}
	if !p.applyRichStreamEvent(st, agentkit.AssistantMessageEvent{
		Type:         agentkit.AssistantEventToolCallStart,
		ContentIndex: sharedIdx,
		ID:           "call_b",
		ToolName:     "Grep",
	}) {
		t.Fatal("tool b start")
	}
	if !p.applyRichStreamEvent(st, agentkit.AssistantMessageEvent{
		Type:         agentkit.AssistantEventToolCallEnd,
		ContentIndex: sharedIdx,
		ID:           "call_a",
	}) {
		t.Fatal("tool a end")
	}
	if !st.steps[0].Done || st.steps[1].Done {
		t.Fatalf("expected only first tool done, steps=%#v", st.steps)
	}
}

func TestBuildRichCardKeepsToolPanelAfterFinalizedSteps(t *testing.T) {
	steps := []toolStep{{
		Kind:    toolStepKindTool,
		Name:    "bash",
		Summary: "date",
		Status:  "called",
		Done:    true,
	}}
	card := buildRichCard(cardStatusDone, "", steps, "当前时间", false, 2*time.Second)
	if !strings.Contains(card, "已调用 1 个工具") {
		t.Fatalf("card missing tool panel title: %s", card)
	}
	if !strings.Contains(card, "collapsible_panel") {
		t.Fatalf("card missing collapsible panel: %s", card)
	}
}
