package feishu

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/session"
)

// asyncSubagentCard tracks a dedicated Feishu progress card for background (async) delegation.
type asyncSubagentCard struct {
	mu              sync.Mutex
	jobID           string
	streamKey       agentkit.SessionID
	parentAgentID   agentkit.AgentID
	agent           string
	task            string
	progressHandle  any
	steps           []toolStep
	toolStepIdx     map[int]int
	thinking        string
	status          cardStatus
	progressStarted time.Time
	lastUpdate      time.Time
}

func (p *Platform) asyncSubagentCardEnabled() bool {
	return p.asyncSubagentProgressCard && p.showToolProgress && p.useRichStream() && p.useInteractiveCard
}

func (c *asyncSubagentCard) panelState() *streamState {
	return &streamState{
		steps:             c.steps,
		thinking:          c.thinking,
		status:            c.status,
		progressStartedAt: c.progressStarted,
		toolStepIdx:       c.toolStepIdx,
	}
}

func (p *Platform) asyncSubagentForStream(streamKey agentkit.SessionID) *asyncSubagentCard {
	if !p.asyncSubagentCardEnabled() {
		return nil
	}
	raw, ok := p.asyncSubagentByStream.Load(streamKey)
	if !ok {
		return nil
	}
	jobID, _ := raw.(string)
	if jobID == "" {
		return nil
	}
	cardRaw, ok := p.asyncSubagentByJob.Load(jobID)
	if !ok {
		return nil
	}
	return cardRaw.(*asyncSubagentCard)
}

func (p *Platform) asyncSubagentForEvent(streamKey agentkit.SessionID, event agentkit.OutboundEvent) *asyncSubagentCard {
	card := p.asyncSubagentForStream(streamKey)
	if card == nil {
		return nil
	}
	if card.parentAgentID != "" && event.AgentID == card.parentAgentID {
		return nil
	}
	switch event.Type {
	case agentkit.EventSubagentStart, agentkit.EventSubagentEnd, agentkit.EventTurnStart, agentkit.EventTurnEnd,
		agentkit.EventMessageStart, agentkit.EventMessageEnd:
		return nil
	}
	return card
}

func (p *Platform) registerAsyncSubagentCard(card *asyncSubagentCard) {
	p.asyncSubagentByJob.Store(card.jobID, card)
	p.asyncSubagentByStream.Store(card.streamKey, card.jobID)
}

func (p *Platform) unregisterAsyncSubagentCard(card *asyncSubagentCard) {
	if card == nil {
		return
	}
	p.asyncSubagentByJob.Delete(card.jobID)
	p.asyncSubagentByStream.Delete(card.streamKey)
}

func (p *Platform) handleAsyncSubagentStart(ctx context.Context, streamKey agentkit.SessionID, parentAgent agentkit.AgentID, data session.SubagentStartData) error {
	jobID := strings.TrimSpace(data.JobID)
	if jobID == "" {
		jobID = strings.TrimSpace(data.Session)
	}
	if jobID == "" {
		return nil
	}
	if existing := p.asyncSubagentForStream(streamKey); existing != nil {
		p.unregisterAsyncSubagentCard(existing)
	}

	agent := subagentDisplayName(data.Agent)
	task := truncateRunes(strings.TrimSpace(data.Task), maxToolSummaryRunes)
	card := &asyncSubagentCard{
		jobID:           jobID,
		streamKey:       streamKey,
		parentAgentID:   parentAgent,
		agent:           agent,
		task:            task,
		toolStepIdx:     make(map[int]int),
		status:          cardStatusWorking,
		progressStarted: time.Now(),
	}
	appendSubagentStartStep(p, card.panelState(), agent, data.Task)

	p.registerAsyncSubagentCard(card)
	return p.flushAsyncSubagentCard(ctx, card, true)
}

func (p *Platform) handleAsyncSubagentEnd(ctx context.Context, data session.SubagentEndData) error {
	jobID := strings.TrimSpace(data.JobID)
	if jobID == "" {
		jobID = strings.TrimSpace(data.Session)
	}
	if jobID == "" {
		return nil
	}
	raw, ok := p.asyncSubagentByJob.Load(jobID)
	if !ok {
		return nil
	}
	card := raw.(*asyncSubagentCard)
	appendSubagentEndStep(p, card.panelState(), data)

	card.mu.Lock()
	status := strings.TrimSpace(data.Status)
	switch {
	case status == "failed" || status == "error" || data.Error != "":
		card.status = cardStatusError
	case status == "running":
		card.status = cardStatusWorking
	default:
		card.status = cardStatusDone
	}
	card.mu.Unlock()

	if err := p.flushAsyncSubagentCard(ctx, card, false); err != nil {
		return err
	}
	p.unregisterAsyncSubagentCard(card)
	return nil
}

func (p *Platform) asyncSubagentCardBody(card *asyncSubagentCard, streaming bool) string {
	card.mu.Lock()
	agent := card.agent
	task := card.task
	status := card.status
	card.mu.Unlock()

	var b strings.Builder
	fmt.Fprintf(&b, "**后台子 Agent · %s**\n\n", agent)
	if task != "" {
		b.WriteString(task)
		b.WriteString("\n\n")
	}
	if streaming {
		b.WriteString("_运行中；完整结论将在完成后以新消息送达。_")
		return strings.TrimSpace(b.String())
	}
	switch status {
	case cardStatusError:
		b.WriteString("_子 Agent 已结束（失败）。详情见折叠区或下一条消息。_")
	case cardStatusDone:
		b.WriteString("_子 Agent 已完成；完整结论见下一条消息。_")
	default:
		b.WriteString("_子 Agent 已结束。_")
	}
	return strings.TrimSpace(b.String())
}

func (p *Platform) flushAsyncSubagentCard(ctx context.Context, card *asyncSubagentCard, streaming bool) error {
	rc, ok := p.replyContextForStreamKey(card.streamKey)
	if !ok {
		return nil
	}

	card.mu.Lock()
	panel := card.panelState()
	elapsed := progressElapsed(panel)
	steps := p.renderRichSteps(panel)
	body := p.asyncSubagentCardBody(card, streaming)
	handle := card.progressHandle
	status := card.status
	if streaming && (status == "" || status == cardStatusThinking) {
		status = cardStatusWorking
	}
	card.mu.Unlock()

	content := buildRichCard(status, "", steps, body, streaming, elapsed)
	if strings.TrimSpace(content) == "" || content == " " {
		return nil
	}

	if handle == nil {
		newHandle, err := p.SendPreviewStart(ctx, rc, content)
		if err != nil {
			return err
		}
		card.mu.Lock()
		card.progressHandle = newHandle
		card.lastUpdate = time.Now()
		card.mu.Unlock()
		return nil
	}

	if p.useRichCardPatch() {
		if err := p.patchRichCard(ctx, handle, content); err != nil {
			return err
		}
	} else if err := p.UpdateMessage(ctx, handle, content); err != nil {
		return err
	}
	card.mu.Lock()
	card.lastUpdate = time.Now()
	card.mu.Unlock()
	return nil
}

func (p *Platform) maybeFlushAsyncSubagentCard(ctx context.Context, card *asyncSubagentCard, changed bool) error {
	if !changed {
		return nil
	}
	card.mu.Lock()
	should := card.progressHandle == nil || time.Since(card.lastUpdate) >= streamUpdateInterval
	card.mu.Unlock()
	if !should {
		return nil
	}
	return p.flushAsyncSubagentCard(ctx, card, true)
}

func (p *Platform) applyAsyncSubagentStreamEvent(card *asyncSubagentCard, ame agentkit.AssistantMessageEvent) bool {
	st := card.panelState()
	changed := p.applyRichStreamEvent(st, ame)
	if !changed {
		return false
	}
	card.mu.Lock()
	card.steps = st.steps
	card.thinking = st.thinking
	card.toolStepIdx = st.toolStepIdx
	card.status = st.status
	card.mu.Unlock()
	return true
}

func (p *Platform) applyAsyncSubagentToolResult(card *asyncSubagentCard, result agentkit.ToolResult) bool {
	st := card.panelState()
	changed := p.applyToolResult(st, result)
	if !changed {
		return false
	}
	card.mu.Lock()
	card.steps = st.steps
	card.status = st.status
	card.mu.Unlock()
	return true
}

func (p *Platform) handleAsyncSubagentStreamUpdate(ctx context.Context, streamKey agentkit.SessionID, event agentkit.OutboundEvent, ame agentkit.AssistantMessageEvent) (bool, error) {
	card := p.asyncSubagentForEvent(streamKey, event)
	if card == nil {
		return false, nil
	}
	seg := streamSegmentOfAssistantEvent(p, ame)
	if seg == streamSegmentNone || seg == streamSegmentBody {
		return true, nil
	}
	changed := p.applyAsyncSubagentStreamEvent(card, ame)
	return true, p.maybeFlushAsyncSubagentCard(ctx, card, changed)
}

func (p *Platform) handleAsyncSubagentToolResult(ctx context.Context, streamKey agentkit.SessionID, event agentkit.OutboundEvent, result agentkit.ToolResult) (bool, error) {
	card := p.asyncSubagentForEvent(streamKey, event)
	if card == nil {
		return false, nil
	}
	changed := p.applyAsyncSubagentToolResult(card, result)
	return true, p.maybeFlushAsyncSubagentCard(ctx, card, changed)
}
