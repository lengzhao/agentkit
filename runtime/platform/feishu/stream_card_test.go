package feishu

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/lengzhao/agentkit"
	capsession "github.com/lengzhao/agentkit/cap/session"
	"github.com/lengzhao/agentkit/runtime/rctx"
)

func TestRenderProgressMarkdownMergesThinkingAndTool(t *testing.T) {
	p := &Platform{
		progressStyle:    "card",
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
	p := &Platform{progressStyle: "card", showToolProgress: true}
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
		progressStyle:      "card",
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
		progressStyle:    "card",
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

func TestLegacyStreamUpdateIgnoresThinkingByDefault(t *testing.T) {
	p := &Platform{progressStyle: "legacy", showThinking: false}
	st := streamStateLiteral(streamStateData{})
	if p.applyRichStreamEvent(st, agentkit.AssistantMessageEvent{
		Type:  agentkit.AssistantEventThinkingDelta,
		Delta: "secret",
	}) {
		t.Fatal("legacy mode should ignore thinking when disabled")
	}
}

func TestRichCardMessageStartPreservesToolSteps(t *testing.T) {
	p := &Platform{progressStyle: "card", useInteractiveCard: true, showToolProgress: true}
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
	p := &Platform{progressStyle: "card", useInteractiveCard: false}
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
	p := &Platform{progressStyle: "card", useInteractiveCard: false}
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
	p := &Platform{progressStyle: "card", useInteractiveCard: false}
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
	p := &Platform{progressStyle: "card", useInteractiveCard: true}
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

func TestScheduleBodyFlushSetsTimerOnce(t *testing.T) {
	p := &Platform{progressStyle: "card"}
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
