package feishu

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/session"
)

// asyncSubagentCard tracks a dedicated Feishu progress card for background (async) delegation.
//
// The progress panel state (steps / thinking / status / toolStepIdx / progressStartedAt)
// lives directly in the embedded *streamState, so the shared render/apply helpers
// (applyRichStreamEvent, applyToolResult, renderRichSteps, progressElapsed) operate on
// the real state in place — no field duplication and no panelState() round-trip.
type asyncSubagentCard struct {
	state          *streamState
	jobID          string
	streamKey      agentkit.SessionID
	parentAgentID  agentkit.AgentID
	agent          string
	task           string
	progressHandle any
	lastUpdate     time.Time
}

func (p *Platform) asyncSubagentCardEnabled() bool {
	return p.asyncSubagentProgressCard && p.showToolProgress && p.useRichStream() && p.useInteractiveCard
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
		state:         newStreamState(),
		jobID:         jobID,
		streamKey:     streamKey,
		parentAgentID: parentAgent,
		agent:         agent,
		task:          task,
	}
	card.state.lock()
	card.state.toolStepIdx = make(map[int]int)
	card.state.status = cardStatusWorking
	card.state.progressStartedAt = time.Now()
	appendSubagentStartStep(p, card.state, agent, data.Task)
	card.state.unlock()

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

	card.state.lock()
	appendSubagentEndStep(p, card.state, data)
	status := strings.TrimSpace(data.Status)
	switch {
	case status == "failed" || status == "error" || data.Error != "":
		card.state.status = cardStatusError
	case status == "running":
		card.state.status = cardStatusWorking
	default:
		card.state.status = cardStatusDone
	}
	card.state.unlock()

	if err := p.flushAsyncSubagentCard(ctx, card, false); err != nil {
		return err
	}
	p.unregisterAsyncSubagentCard(card)
	return nil
}

// asyncSubagentCardBody renders the main_text body for the async subagent card.
// Caller must hold card.state.mu.
func (p *Platform) asyncSubagentCardBody(card *asyncSubagentCard, streaming bool) string {
	agent := card.agent
	task := card.task
	status := card.state.status

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

	card.state.lock()
	elapsed := progressElapsed(card.state)
	steps := p.renderRichSteps(card.state)
	body := p.asyncSubagentCardBody(card, streaming)
	handle := card.progressHandle
	status := card.state.status
	if streaming && (status == "" || status == cardStatusThinking) {
		status = cardStatusWorking
	}
	card.state.unlock()

	content := buildRichCard(status, "", steps, body, streaming, elapsed)
	if strings.TrimSpace(content) == "" || content == " " {
		return nil
	}

	if handle == nil {
		newHandle, err := p.SendPreviewStart(ctx, rc, content)
		if err != nil {
			return err
		}
		card.state.lock()
		card.progressHandle = newHandle
		card.lastUpdate = time.Now()
		card.state.unlock()
		return nil
	}

	if err := p.patchRichCard(ctx, handle, content); err != nil {
		return err
	}
	card.state.lock()
	card.lastUpdate = time.Now()
	card.state.unlock()
	return nil
}

func (p *Platform) maybeFlushAsyncSubagentCard(ctx context.Context, card *asyncSubagentCard, changed bool) error {
	if !changed {
		return nil
	}
	card.state.lock()
	should := card.progressHandle == nil || time.Since(card.lastUpdate) >= streamUpdateInterval
	card.state.unlock()
	if !should {
		return nil
	}
	return p.flushAsyncSubagentCard(ctx, card, true)
}

func (p *Platform) applyAsyncSubagentStreamEvent(card *asyncSubagentCard, ame agentkit.AssistantMessageEvent) bool {
	card.state.lock()
	changed := p.applyRichStreamEvent(card.state, ame)
	card.state.unlock()
	return changed
}

func (p *Platform) applyAsyncSubagentToolResult(card *asyncSubagentCard, result agentkit.ToolResult) bool {
	card.state.lock()
	changed := p.applyToolResult(card.state, result)
	card.state.unlock()
	return changed
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
