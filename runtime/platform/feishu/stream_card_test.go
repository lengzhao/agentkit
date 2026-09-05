package feishu

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/lengzhao/agentkit"
)

func TestApplyRichStreamEventToolAndThinking(t *testing.T) {
	p := &Platform{
		progressStyle:    "card",
		showThinking:     true,
		showToolProgress: true,
	}
	st := &streamState{toolStepIdx: make(map[int]int)}

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

func TestRenderProgressContentOmitsBodyText(t *testing.T) {
	p := &Platform{
		progressStyle:    "card",
		showThinking:     true,
		showToolProgress: true,
	}
	st := &streamState{
		status:   cardStatusWorking,
		bodyText: "final answer",
		thinking: "hmm",
		steps: []toolStep{{
			Kind:    toolStepKindTool,
			Name:    "Grep",
			Summary: "pattern",
			Done:    true,
		}},
		progressStartedAt: stTime(),
	}
	card := p.renderProgressContent(st, true)
	if !strings.Contains(card, `"schema"`) {
		t.Fatalf("expected card json, got %q", card)
	}
	if strings.Contains(card, "final answer") {
		t.Fatalf("progress card should not carry body text: %q", card)
	}
	if !strings.Contains(card, "collapsible_panel") {
		t.Fatalf("expected progress panel in card: %q", card)
	}
}

func stTime() time.Time {
	return time.Unix(0, 0)
}

func TestLegacyStreamUpdateIgnoresThinkingByDefault(t *testing.T) {
	p := &Platform{progressStyle: "legacy", showThinking: false}
	st := &streamState{}
	if p.applyRichStreamEvent(st, agentkit.AssistantMessageEvent{
		Type:  agentkit.AssistantEventThinkingDelta,
		Delta: "secret",
	}) {
		t.Fatal("legacy mode should ignore thinking when disabled")
	}
}

func TestRenderProgressContentKeepsRecentProgressOnly(t *testing.T) {
	p := &Platform{
		progressStyle:    "card",
		showThinking:     true,
		showToolProgress: true,
	}
	st := &streamState{
		thinking:          "latest thought",
		progressStartedAt: time.Now(),
		startedAt:         time.Now(),
		steps: []toolStep{
			{Kind: toolStepKindTool, Name: "Read", Summary: "a.go"},
			{Kind: toolStepKindToolResult, Name: "Read", Result: "file-a"},
			{Kind: toolStepKindTool, Name: "Grep", Summary: "pattern"},
			{Kind: toolStepKindToolResult, Name: "Grep", Result: "match"},
		},
	}
	card := p.renderProgressContent(st, true)
	if strings.Contains(card, "Read") {
		t.Fatalf("expected older Read entries to be dropped, got %q", card)
	}
	if !strings.Contains(card, "Grep") {
		t.Fatalf("expected latest Grep entries to remain, got %q", card)
	}
}

func TestRenderCompactProgressCardKeepsRecentProgressOnly(t *testing.T) {
	p := &Platform{
		progressStyle:    "compact",
		showThinking:     true,
		showToolProgress: true,
	}
	st := &streamState{
		thinking: "plan",
		steps: []toolStep{
			{
				Kind:    toolStepKindTool,
				Name:    "Bash",
				Summary: "ls",
				Done:    true,
				Status:  "called",
			},
			{
				Kind:    toolStepKindToolResult,
				Name:    "Bash",
				Result:  "README.md",
				Status:  "completed",
				Done:    true,
				Success: boolPtr(true),
			},
		},
	}
	content := p.renderProgressContent(st, false)
	if !strings.HasPrefix(content, "__cc_connect_progress_card_v1__:") {
		t.Fatalf("expected compact payload prefix, got %q", content)
	}
	if !strings.Contains(content, `"kind":"tool_use"`) || !strings.Contains(content, `"kind":"tool_result"`) {
		t.Fatalf("expected separate tool_use and tool_result entries, got %q", content)
	}
}

func TestRenderCompactProgressCardTruncatesOlderEntries(t *testing.T) {
	p := &Platform{
		progressStyle:    "compact",
		showThinking:     true,
		showToolProgress: true,
	}
	st := &streamState{
		thinking: "old thought",
		steps: []toolStep{
			{Kind: toolStepKindTool, Name: "Read", Summary: "a.go", Done: true, Status: "called"},
			{Kind: toolStepKindToolResult, Name: "Read", Result: "file-a", Done: true, Status: "completed", Success: boolPtr(true)},
			{Kind: toolStepKindTool, Name: "Grep", Summary: "pattern", Done: true, Status: "called"},
			{Kind: toolStepKindToolResult, Name: "Grep", Result: "match", Done: true, Status: "completed", Success: boolPtr(true)},
		},
	}
	content := p.renderProgressContent(st, false)
	if !strings.Contains(content, `"truncated":true`) {
		t.Fatalf("expected truncated flag, got %q", content)
	}
	if strings.Contains(content, `"tool":"Read"`) {
		t.Fatalf("expected older Read entries to be dropped, got %q", content)
	}
	if !strings.Contains(content, `"tool":"Grep"`) {
		t.Fatalf("expected latest Grep entries to remain, got %q", content)
	}
}

func TestRenderCompactProgressCardOmitsBodyText(t *testing.T) {
	p := &Platform{
		progressStyle:    "compact",
		showToolProgress: true,
	}
	st := &streamState{
		bodyText: "正文应在正文卡中展示",
		steps: []toolStep{{
			Kind:    toolStepKindTool,
			Name:    "Read",
			Summary: "a.go",
			Done:    true,
			Status:  "called",
		}},
	}
	content := p.renderProgressContent(st, true)
	if strings.Contains(content, "正文应在正文卡中展示") {
		t.Fatalf("progress card should not carry body text, got %q", content)
	}
}

func TestHandleRichStreamMessageStartResetsMessageState(t *testing.T) {
	p := &Platform{progressStyle: "card"}
	sessionID := agentkit.SessionID("session-1")
	st := p.streamState(sessionID)
	st.mu.Lock()
	st.thinking = "old"
	st.steps = []toolStep{{Kind: toolStepKindTool, Name: "Read"}}
	st.bodyText = "partial"
	st.bodyHandle = &feishuPreviewHandle{messageID: "body"}
	st.progressHandle = &feishuPreviewHandle{messageID: "progress"}
	st.cards = []streamCard{
		{Kind: streamCardProgress, Handle: st.progressHandle},
		{Kind: streamCardBody, Handle: st.bodyHandle},
	}
	st.mu.Unlock()

	if err := p.handleRichStreamMessageStart(context.Background(), sessionID); err != nil {
		t.Fatalf("handleRichStreamMessageStart: %v", err)
	}

	st.mu.Lock()
	defer st.mu.Unlock()
	if st.thinking != "" || len(st.steps) != 0 || st.bodyText != "" || st.bodyHandle != nil || st.progressHandle != nil {
		t.Fatalf("expected message state reset, got thinking=%q steps=%d bodyText=%q bodyHandle=%v progressHandle=%v",
			st.thinking, len(st.steps), st.bodyText, st.bodyHandle, st.progressHandle)
	}
	if len(st.cards) != 2 {
		t.Fatalf("expected prior cards to remain queued, got %d", len(st.cards))
	}
}

func TestEvictStreamCardsDropsOldestProgress(t *testing.T) {
	p := &Platform{progressStyle: "card", useInteractiveCard: false}
	p1 := &feishuPreviewHandle{messageID: "p1"}
	st := &streamState{
		cards: []streamCard{
			{Kind: streamCardProgress, Handle: p1},
			{Kind: streamCardBody, Handle: &feishuPreviewHandle{messageID: "b1"}},
			{Kind: streamCardProgress, Handle: &feishuPreviewHandle{messageID: "p2"}},
			{Kind: streamCardBody, Handle: &feishuPreviewHandle{messageID: "b2"}},
		},
		progressHandle: p1,
	}
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
	st := &streamState{
		cards: []streamCard{
			{Kind: streamCardBody, Handle: &feishuPreviewHandle{messageID: "b1"}},
			{Kind: streamCardProgress, Handle: p1},
			{Kind: streamCardBody, Handle: &feishuPreviewHandle{messageID: "b2"}},
		},
		progressHandle: p1,
	}
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

func TestEvictStreamCardsKeepsBodyAtHead(t *testing.T) {
	p := &Platform{progressStyle: "card", useInteractiveCard: false}
	st := &streamState{
		cards: []streamCard{
			{Kind: streamCardBody, Handle: &feishuPreviewHandle{messageID: "b1"}},
			{Kind: streamCardProgress, Handle: &feishuPreviewHandle{messageID: "p1"}},
			{Kind: streamCardBody, Handle: &feishuPreviewHandle{messageID: "b2"}},
			{Kind: streamCardProgress, Handle: &feishuPreviewHandle{messageID: "p2"}},
		},
	}
	p.evictStreamCards(context.Background(), st)
	if len(st.cards) != 4 {
		t.Fatalf("cards len = %d, want 4 when oldest is body", len(st.cards))
	}
}

func TestHandleRichBodyDeltaKeepsProgressState(t *testing.T) {
	p := &Platform{progressStyle: "card"}
	sessionID := agentkit.SessionID("session-body-delta")
	st := p.streamState(sessionID)
	st.mu.Lock()
	st.progressHandle = &feishuPreviewHandle{messageID: "progress"}
	st.steps = []toolStep{{Kind: toolStepKindTool, Name: "Read", Summary: "a.go"}}
	st.thinking = "plan"
	st.toolStepIdx = make(map[int]int)
	st.progressStartedAt = time.Now()
	st.startedAt = time.Now()
	st.mu.Unlock()

	if err := p.handleRichBodyDelta(context.Background(), sessionID, "hello"); err != nil {
		t.Fatal(err)
	}

	st.mu.Lock()
	defer st.mu.Unlock()
	if len(st.steps) != 1 || st.thinking != "plan" || st.progressHandle == nil {
		t.Fatalf("progress state changed unexpectedly: %#v", st)
	}
	if st.bodyText != "hello" {
		t.Fatalf("bodyText = %q", st.bodyText)
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

func TestScheduleBodyFlushSetsTimerOnce(t *testing.T) {
	p := &Platform{progressStyle: "card"}
	sessionID := agentkit.SessionID("session-body-timer")
	st := p.streamState(sessionID)
	st.mu.Lock()
	st.bodyHandle = &feishuPreviewHandle{messageID: "body"}
	st.lastBodyUpdate = time.Now()
	st.mu.Unlock()

	p.scheduleBodyFlush(sessionID)
	st.mu.Lock()
	first := st.bodyFlushTimer
	st.mu.Unlock()
	if first == nil {
		t.Fatal("expected body flush timer")
	}

	p.scheduleBodyFlush(sessionID)
	st.mu.Lock()
	second := st.bodyFlushTimer
	st.mu.Unlock()
	if first != second {
		t.Fatal("expected pending body flush timer to be reused")
	}
	first.Stop()
}

func boolPtr(v bool) *bool {
	return &v
}
