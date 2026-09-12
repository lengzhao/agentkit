package feishu

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/platform/common"
	"github.com/lengzhao/agentkit/runtime/session"
)

const maxToolSummaryRunes = 180
const maxRecentProgressSteps = 2
const streamHeartbeatInterval = 10 * time.Second // CardKit 统一卡：reaction 轮换 + 处理过程时间戳

func formatStreamHeartbeatTimestamp(t time.Time) string {
	return t.Format("2006-01-02 15:04:05")
}

func formatProgressHeartbeatLine(t time.Time) string {
	return "> ⏱ " + formatStreamHeartbeatTimestamp(t)
}

func joinMarkdownBlocks(blocks ...string) string {
	var b strings.Builder
	for _, block := range blocks {
		block = strings.TrimSpace(block)
		if block == "" {
			continue
		}
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		b.WriteString(block)
	}
	return b.String()
}

func (p *Platform) unifiedProgressStreamMarkdown(st *streamState, streaming bool) string {
	core := p.renderProgressMarkdown(st, streaming)
	return joinMarkdownBlocks(core, strings.Join(st.progressHeartbeatLines, "\n"))
}

func unifiedBodyStreamMarkdown(st *streamState) string {
	return joinMarkdownBlocks(st.bodyText, strings.Join(st.bodyHeartbeatLines, "\n"))
}

func unifiedTurnEndBodyMarkdown(st *streamState) string {
	body := unifiedBodyStreamMarkdown(st)
	if strings.TrimSpace(body) == "" {
		return st.lastStreamedBody
	}
	return body
}

func (p *Platform) unifiedTurnEndProgressMarkdown(st *streamState) string {
	md := p.renderProgressMarkdown(st, false)
	return mergeProgressHeartbeatsBeforeFooter(md, st.progressHeartbeatLines)
}

const progressMarkdownFooterSeparator = "\n\n---\n\n"

func mergeProgressHeartbeatsBeforeFooter(progressMD string, lines []string) string {
	if len(lines) == 0 {
		return progressMD
	}
	block := strings.Join(lines, "\n")
	if i := strings.Index(progressMD, progressMarkdownFooterSeparator); i >= 0 {
		prefix := strings.TrimRight(progressMD[:i], "\n")
		suffix := progressMD[i+len(progressMarkdownFooterSeparator):]
		return joinMarkdownBlocks(prefix, block) + progressMarkdownFooterSeparator + suffix
	}
	return joinMarkdownBlocks(progressMD, block)
}

func subagentToolLabel(agent string) string {
	return "子Agent:" + strings.TrimSpace(agent)
}

func (p *Platform) startProgressHeartbeat(sessionID agentkit.SessionID) {
	st := p.streamState(sessionID)
	ctx, cancel := context.WithCancel(context.Background())

	st.mu.Lock()
	if st.heartbeatStop != nil {
		st.heartbeatStop()
	}
	st.heartbeatStop = cancel
	st.mu.Unlock()

	go p.runProgressHeartbeat(ctx, sessionID)
}

func (p *Platform) runProgressHeartbeat(ctx context.Context, sessionID agentkit.SessionID) {
	ticker := time.NewTicker(streamHeartbeatInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.tickUnifiedStreamHeartbeat(sessionID)
		}
	}
}

func (p *Platform) tickUnifiedStreamHeartbeat(sessionID agentkit.SessionID) {
	if !p.useUnifiedStreamCard() {
		return
	}
	raw, ok := p.streams.Load(sessionID)
	if !ok {
		return
	}
	st := raw.(*streamState)
	if !shouldCardReactionHeartbeat(st) {
		return
	}
	p.rotateCardReplyReaction(sessionID)
	if err := p.flushUnifiedStreamTimestamp(context.Background(), sessionID); err != nil {
		if isCardStreamingClosedError(err) {
			return
		}
		slog.Debug(p.tag()+": stream timestamp heartbeat failed", "session_id", sessionID, "error", err)
	}
}

func (p *Platform) flushUnifiedStreamTimestamp(ctx context.Context, sessionID agentkit.SessionID) error {
	showProgress := p.showStreamProgress()
	st := p.streamState(sessionID)
	st.mu.Lock()
	if st.unifiedTextFallback {
		st.mu.Unlock()
		return nil
	}
	line := formatProgressHeartbeatLine(time.Now())
	if showProgress {
		st.progressHeartbeatLines = append(st.progressHeartbeatLines, line)
	} else {
		st.bodyHeartbeatLines = append(st.bodyHeartbeatLines, line)
	}
	st.mu.Unlock()

	if showProgress {
		return p.flushUnifiedProgress(ctx, sessionID)
	}
	return p.flushUnifiedBody(ctx, sessionID)
}

func shouldFlushUnifiedProgress(changed bool, lastUpdate time.Time, eventType agentkit.AssistantMessageEventType) bool {
	if !changed {
		return false
	}
	switch eventType {
	case agentkit.AssistantEventToolCallStart, agentkit.AssistantEventToolCallDelta:
		return false
	case agentkit.AssistantEventToolCallEnd:
		return true
	default:
		return lastUpdate.IsZero() || time.Since(lastUpdate) >= streamUpdateInterval
	}
}

func (p *Platform) useRichStream() bool {
	switch p.progressStyle {
	case "card", "compact":
		return true
	default:
		return false
	}
}

func (p *Platform) useUnifiedStreamCard() bool {
	return p.useRichStream() && p.useCardKitStream()
}

func (p *Platform) showStreamProgress() bool {
	return p.showThinking || p.showToolProgress
}

func (p *Platform) richStreamState(sessionID agentkit.SessionID) *streamState {
	st := p.streamState(sessionID)
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.startedAt.IsZero() {
		st.startedAt = time.Now()
		st.status = cardStatusThinking
		st.toolStepIdx = make(map[int]int)
	}
	if st.progressStartedAt.IsZero() {
		st.progressStartedAt = time.Now()
	}
	return st
}

func (p *Platform) bodyStreamInterval() time.Duration {
	if p.useCardKitStream() {
		return streamCardKitInterval
	}
	return streamUpdateInterval
}

func (p *Platform) finalizeBodyCard(ctx context.Context, handle any, bodyText string) error {
	if strings.TrimSpace(bodyText) == "" {
		return nil
	}
	if h, ok := handle.(*feishuPreviewHandle); ok && h.cardID != "" && h.elementID != "" {
		return p.finalizeStreamingBodyCard(ctx, h, bodyText)
	}
	return p.UpdateMessage(ctx, handle, buildFinalPreviewCardJSON(bodyText))
}

func (p *Platform) handleRichStreamMessageStart(_ context.Context, sessionID agentkit.SessionID) error {
	p.cancelBodyFlushTimer(sessionID)
	st := p.richStreamState(sessionID)
	st.mu.Lock()
	st.bodyText = ""
	if p.useUnifiedStreamCard() {
		st.toolStepIdx = make(map[int]int)
		if st.status == "" || st.status == cardStatusThinking {
			st.status = cardStatusWorking
		}
		st.mu.Unlock()
		return nil
	}
	st.thinking = ""
	st.steps = nil
	st.toolStepIdx = make(map[int]int)
	st.bodyHandle = nil
	st.progressHandle = nil
	st.activeSegment = streamSegmentNone
	st.status = cardStatusThinking
	st.progressStartedAt = time.Now()
	st.mu.Unlock()
	return nil
}

func streamSegmentOfAssistantEvent(p *Platform, ame agentkit.AssistantMessageEvent) streamSegmentKind {
	switch ame.Type {
	case agentkit.AssistantEventTextDelta:
		return streamSegmentBody
	case agentkit.AssistantEventThinkingStart, agentkit.AssistantEventThinkingDelta, agentkit.AssistantEventThinkingEnd:
		if !p.showThinking {
			return streamSegmentNone
		}
		return streamSegmentThinking
	case agentkit.AssistantEventToolCallStart, agentkit.AssistantEventToolCallDelta, agentkit.AssistantEventToolCallEnd:
		if !p.showToolProgress {
			return streamSegmentNone
		}
		return streamSegmentTool
	default:
		return streamSegmentNone
	}
}

func (p *Platform) switchRichSegment(ctx context.Context, sessionID agentkit.SessionID, next streamSegmentKind) error {
	if next == streamSegmentNone {
		return nil
	}
	st := p.richStreamState(sessionID)
	st.mu.Lock()
	prev := st.activeSegment
	if prev == next {
		st.mu.Unlock()
		return nil
	}
	progressHandle := st.progressHandle
	bodyHandle := st.bodyHandle
	bodyText := st.bodyText
	st.mu.Unlock()

	if prev == streamSegmentBody {
		p.cancelBodyFlushTimer(sessionID)
	}
	if p.client != nil {
		if prev == streamSegmentBody && bodyHandle != nil && strings.TrimSpace(bodyText) != "" {
			if err := p.finalizeBodyCard(ctx, bodyHandle, bodyText); err != nil {
				slog.Debug(p.tag()+": finalize body card on segment switch failed", "session_id", sessionID, "error", err)
			}
		}
		if (prev == streamSegmentThinking || prev == streamSegmentTool) && progressHandle != nil {
			if err := p.finalizeProgressCard(ctx, sessionID, progressHandle); err != nil {
				slog.Debug(p.tag()+": finalize progress card on segment switch failed", "session_id", sessionID, "error", err)
			}
		}
	}

	st.mu.Lock()
	st.activeSegment = next
	st.progressHandle = nil
	st.bodyHandle = nil
	st.bodyText = ""
	if next != streamSegmentBody {
		st.thinking = ""
		st.steps = nil
		st.toolStepIdx = make(map[int]int)
		st.progressStartedAt = time.Now()
	}
	st.mu.Unlock()
	return nil
}

func (p *Platform) handleRichStreamUpdate(ctx context.Context, sessionID agentkit.SessionID, ame agentkit.AssistantMessageEvent) error {
	if p.useUnifiedStreamCard() {
		seg := streamSegmentOfAssistantEvent(p, ame)
		if seg == streamSegmentBody {
			return p.handleRichBodyDelta(ctx, sessionID, ame.Delta)
		}
		if seg == streamSegmentNone {
			return nil
		}
		st := p.richStreamState(sessionID)
		st.mu.Lock()
		changed := p.applyRichStreamEvent(st, ame)
		shouldFlush := shouldFlushUnifiedProgress(changed, st.lastProgressUpdate, ame.Type)
		st.mu.Unlock()
		if !shouldFlush {
			return nil
		}
		return p.flushUnifiedProgress(ctx, sessionID)
	}

	seg := streamSegmentOfAssistantEvent(p, ame)
	if seg == streamSegmentBody {
		return p.handleRichBodyDelta(ctx, sessionID, ame.Delta)
	}
	if seg == streamSegmentNone {
		return nil
	}
	if err := p.switchRichSegment(ctx, sessionID, seg); err != nil {
		return err
	}

	st := p.richStreamState(sessionID)
	st.mu.Lock()
	changed := p.applyRichStreamEvent(st, ame)
	shouldFlush := changed && (st.progressHandle == nil || time.Since(st.lastProgressUpdate) >= streamUpdateInterval)
	st.mu.Unlock()
	if !shouldFlush {
		return nil
	}
	return p.flushProgressCard(ctx, sessionID, true)
}

func stopStreamTimer(t **time.Timer) {
	if *t != nil {
		(*t).Stop()
		*t = nil
	}
}

func streamFlushDelay(lastUpdate time.Time, interval time.Duration) time.Duration {
	if lastUpdate.IsZero() {
		return 0
	}
	elapsed := time.Since(lastUpdate)
	if elapsed >= interval {
		return 0
	}
	return interval - elapsed
}

func (p *Platform) cancelBodyFlushTimer(sessionID agentkit.SessionID) {
	st := p.streamState(sessionID)
	st.mu.Lock()
	stopStreamTimer(&st.bodyFlushTimer)
	st.mu.Unlock()
}

func (p *Platform) cancelLegacyFlushTimer(sessionID agentkit.SessionID) {
	st := p.streamState(sessionID)
	st.mu.Lock()
	stopStreamTimer(&st.legacyFlushTimer)
	st.mu.Unlock()
}

func (p *Platform) scheduleBodyFlush(sessionID agentkit.SessionID) {
	st := p.streamState(sessionID)
	st.mu.Lock()
	if st.bodyFlushTimer != nil {
		st.mu.Unlock()
		return
	}
	delay := streamFlushDelay(st.lastBodyUpdate, p.bodyStreamInterval())
	sid := sessionID
	st.bodyFlushTimer = time.AfterFunc(delay, func() {
		st.mu.Lock()
		stopStreamTimer(&st.bodyFlushTimer)
		st.mu.Unlock()
		if p.useUnifiedStreamCard() {
			if err := p.flushUnifiedBody(context.Background(), sid); err != nil {
				slog.Debug(p.tag()+": debounced unified body flush failed", "session_id", sid, "error", err)
			}
			return
		}
		if err := p.flushBodyCard(context.Background(), sid, true); err != nil {
			slog.Debug(p.tag()+": debounced body flush failed", "session_id", sid, "error", err)
		}
	})
	st.mu.Unlock()
}

func (p *Platform) scheduleLegacyFlush(sessionID agentkit.SessionID) {
	st := p.streamState(sessionID)
	st.mu.Lock()
	if st.legacyFlushTimer != nil {
		st.mu.Unlock()
		return
	}
	delay := streamFlushDelay(st.lastUpdate, p.bodyStreamInterval())
	sid := sessionID
	st.legacyFlushTimer = time.AfterFunc(delay, func() {
		st.mu.Lock()
		stopStreamTimer(&st.legacyFlushTimer)
		st.mu.Unlock()
		st = p.streamState(sid)
		st.mu.Lock()
		text := st.accumulated
		st.mu.Unlock()
		if strings.TrimSpace(text) == "" {
			return
		}
		if err := p.flushStream(context.Background(), sid, text); err != nil {
			slog.Debug(p.tag()+": debounced legacy flush failed", "session_id", sid, "error", err)
		}
	})
	st.mu.Unlock()
}

func (p *Platform) handleRichBodyDelta(ctx context.Context, sessionID agentkit.SessionID, delta string) error {
	if delta == "" {
		return nil
	}
	if !p.useUnifiedStreamCard() {
		if err := p.switchRichSegment(ctx, sessionID, streamSegmentBody); err != nil {
			return err
		}
	}

	st := p.richStreamState(sessionID)
	st.mu.Lock()
	st.bodyText += delta
	st.status = cardStatusWorking
	var shouldFlushNow bool
	if p.useUnifiedStreamCard() {
		shouldFlushNow = st.cardHandle == nil || time.Since(st.lastBodyUpdate) >= p.bodyStreamInterval()
	} else {
		shouldFlushNow = st.bodyHandle == nil || time.Since(st.lastBodyUpdate) >= p.bodyStreamInterval()
	}
	st.mu.Unlock()
	if shouldFlushNow {
		p.cancelBodyFlushTimer(sessionID)
		if p.useUnifiedStreamCard() {
			return p.flushUnifiedBody(ctx, sessionID)
		}
		return p.flushBodyCard(ctx, sessionID, true)
	}
	p.scheduleBodyFlush(sessionID)
	return nil
}

func (st *streamState) enqueueCard(kind streamCardKind, handle any) {
	st.cards = append(st.cards, streamCard{Kind: kind, Handle: handle})
}

func (st *streamState) clearCardRef(handle any) {
	if st.progressHandle == handle {
		st.progressHandle = nil
	}
	if st.bodyHandle == handle {
		st.bodyHandle = nil
	}
}

func (p *Platform) removePriorProgressCards(ctx context.Context, st *streamState, keep any) {
	remaining := make([]streamCard, 0, len(st.cards))
	for _, card := range st.cards {
		if card.Kind != streamCardProgress || card.Handle == keep {
			remaining = append(remaining, card)
			continue
		}
		if err := p.DeletePreviewMessage(ctx, card.Handle); err != nil {
			slog.Debug(p.tag()+": remove prior progress card failed", "error", err)
		}
		st.clearCardRef(card.Handle)
	}
	st.cards = remaining
}

func (p *Platform) evictStreamCards(ctx context.Context, st *streamState) {
	for len(st.cards) > maxStreamCards {
		oldest := st.cards[0]
		if oldest.Kind == streamCardProgress {
			if err := p.DeletePreviewMessage(ctx, oldest.Handle); err != nil {
				slog.Debug(p.tag()+": evict progress card failed", "error", err)
			}
		}
		st.clearCardRef(oldest.Handle)
		st.cards = st.cards[1:]
	}
}

func (p *Platform) applyRichStreamEvent(st *streamState, ame agentkit.AssistantMessageEvent) bool {
	switch ame.Type {
	case agentkit.AssistantEventThinkingDelta:
		if !p.showThinking || ame.Delta == "" {
			return false
		}
		st.thinking += ame.Delta
		st.status = cardStatusThinking
		return true
	case agentkit.AssistantEventToolCallStart:
		if !p.showToolProgress {
			return false
		}
		name := strings.TrimSpace(ame.ToolName)
		callID := strings.TrimSpace(ame.ID)
		if ame.ToolCall != nil {
			if name == "" {
				name = strings.TrimSpace(ame.ToolCall.Name)
			}
			if callID == "" {
				callID = string(ame.ToolCall.ID)
			}
		}
		if name == "" {
			name = "Tool"
		}
		st.steps = append(st.steps, toolStep{
			Kind:    toolStepKindTool,
			Name:    name,
			Summary: name,
			Status:  "running",
			CallID:  callID,
		})
		idx := len(st.steps) - 1
		st.toolStepIdx[ame.ContentIndex] = idx
		st.status = cardStatusWorking
		return true
	case agentkit.AssistantEventToolCallDelta:
		if !p.showToolProgress {
			return false
		}
		idx, ok := st.toolStepIdx[ame.ContentIndex]
		if !ok || idx < 0 || idx >= len(st.steps) {
			return false
		}
		delta := strings.TrimSpace(ame.Delta)
		if delta == "" {
			return false
		}
		step := st.steps[idx]
		if step.Summary == step.Name || step.Summary == "" {
			step.Summary = truncateRunes(delta, maxToolSummaryRunes)
		} else {
			step.Summary = truncateRunes(step.Summary+delta, maxToolSummaryRunes)
		}
		st.steps[idx] = step
		return true
	case agentkit.AssistantEventToolCallEnd:
		if !p.showToolProgress {
			return false
		}
		idx, ok := st.toolStepIdx[ame.ContentIndex]
		if !ok || idx < 0 || idx >= len(st.steps) {
			return false
		}
		step := st.steps[idx]
		if step.Kind != toolStepKindTool {
			return false
		}
		if ame.ToolCall != nil {
			input := strings.TrimSpace(string(ame.ToolCall.Input))
			if input != "" {
				step.Summary = truncateRunes(input, maxToolSummaryRunes)
			}
			if name := strings.TrimSpace(ame.ToolCall.Name); name != "" {
				step.Name = name
			}
			if callID := string(ame.ToolCall.ID); callID != "" {
				step.CallID = callID
			}
		}
		step.Status = "called"
		step.Done = true
		st.steps[idx] = step
		return true
	default:
		return false
	}
}

func (p *Platform) applyToolResult(st *streamState, result agentkit.ToolResult) bool {
	if !p.showToolProgress {
		return false
	}
	name := strings.TrimSpace(result.Name)
	if name == "" {
		name = "Tool"
	}
	content := truncateRunes(strings.TrimSpace(result.Content), maxToolSummaryRunes)
	success := true
	status := "completed"
	if result.Audit != nil {
		if decision := strings.TrimSpace(result.Audit["decision"]); decision == "deny" {
			success = false
			status = "failed"
		}
	}
	okVal := success
	st.steps = append(st.steps, toolStep{
		Kind:    toolStepKindToolResult,
		Name:    name,
		Summary: content,
		Result:  content,
		Status:  status,
		CallID:  string(result.ID),
		Success: &okVal,
		Done:    true,
	})
	st.status = cardStatusWorking
	return true
}

func (p *Platform) handleRichToolResult(ctx context.Context, event agentkit.OutboundEvent) error {
	var result agentkit.ToolResult
	if err := json.Unmarshal(event.Data, &result); err != nil {
		return err
	}
	if !p.showToolProgress {
		return nil
	}
	sessionID := session.OutboundRouteID(event)
	if p.useUnifiedStreamCard() {
		st := p.richStreamState(sessionID)
		st.mu.Lock()
		changed := p.applyToolResult(st, result)
		shouldFlush := changed
		st.mu.Unlock()
		if !shouldFlush {
			return nil
		}
		return p.flushUnifiedProgress(ctx, sessionID)
	}
	if err := p.switchRichSegment(ctx, sessionID, streamSegmentTool); err != nil {
		return err
	}
	st := p.richStreamState(sessionID)
	st.mu.Lock()
	changed := p.applyToolResult(st, result)
	shouldFlush := changed && (st.progressHandle == nil || time.Since(st.lastProgressUpdate) >= streamUpdateInterval)
	st.mu.Unlock()
	if !shouldFlush {
		return nil
	}
	return p.flushProgressCard(ctx, sessionID, true)
}

func (p *Platform) handleRichSubagentEvent(ctx context.Context, event agentkit.OutboundEvent) error {
	if !p.showToolProgress {
		return nil
	}
	sessionID := session.OutboundRouteID(event)
	if p.useUnifiedStreamCard() {
		st := p.richStreamState(sessionID)
		st.mu.Lock()
		changed := false
		switch event.Type {
		case agentkit.EventSubagentStart:
			var data session.SubagentStartData
			if err := json.Unmarshal(event.Data, &data); err != nil {
				st.mu.Unlock()
				return err
			}
			appendSubagentStartStep(st, data.Agent, data.Task)
			changed = true
		case agentkit.EventSubagentEnd:
			var data session.SubagentEndData
			if err := json.Unmarshal(event.Data, &data); err != nil {
				st.mu.Unlock()
				return err
			}
			appendSubagentEndStep(st, data)
			changed = true
		}
		shouldFlush := changed
		st.mu.Unlock()
		if !shouldFlush {
			return nil
		}
		return p.flushUnifiedProgress(ctx, sessionID)
	}
	if err := p.switchRichSegment(ctx, sessionID, streamSegmentTool); err != nil {
		return err
	}
	st := p.richStreamState(sessionID)
	st.mu.Lock()
	changed := false
	switch event.Type {
	case agentkit.EventSubagentStart:
		var data session.SubagentStartData
		if err := json.Unmarshal(event.Data, &data); err != nil {
			st.mu.Unlock()
			return err
		}
		appendSubagentStartStep(st, data.Agent, data.Task)
		changed = true
	case agentkit.EventSubagentEnd:
		var data session.SubagentEndData
		if err := json.Unmarshal(event.Data, &data); err != nil {
			st.mu.Unlock()
			return err
		}
		appendSubagentEndStep(st, data)
		changed = true
	}
	shouldFlush := changed && (st.progressHandle == nil || time.Since(st.lastProgressUpdate) >= streamUpdateInterval)
	st.mu.Unlock()
	if !shouldFlush {
		return nil
	}
	return p.flushProgressCard(ctx, sessionID, true)
}

func subagentDisplayName(agent string) string {
	agent = strings.TrimSpace(agent)
	if agent == "" {
		return "subagent"
	}
	return agent
}

func appendSubagentStartStep(st *streamState, agent, task string) {
	agent = subagentDisplayName(agent)
	task = truncateRunes(strings.TrimSpace(task), maxToolSummaryRunes)
	if task == "" {
		task = agent
	}
	st.steps = append(st.steps, toolStep{
		Kind:    toolStepKindSubagent,
		Name:    agent,
		Summary: task,
		Status:  "running",
	})
	st.status = cardStatusWorking
}

func appendSubagentEndStep(st *streamState, data session.SubagentEndData) {
	agent := subagentDisplayName(data.Agent)
	summary := truncateRunes(strings.TrimSpace(data.Summary), maxToolSummaryRunes)
	if summary == "" && data.Error != "" {
		summary = truncateRunes(strings.TrimSpace(data.Error), maxToolSummaryRunes)
	}
	status := strings.TrimSpace(data.Status)
	if status == "" {
		status = "completed"
	}
	success := status != "failed" && status != "error"
	okVal := success
	st.steps = append(st.steps, toolStep{
		Kind:    toolStepKindSubagent,
		Name:    agent,
		Summary: summary,
		Result:  summary,
		Status:  status,
		Success: &okVal,
		Done:    true,
	})
	st.status = cardStatusWorking
}

func (p *Platform) handleRichStreamMessageEnd(ctx context.Context, event agentkit.OutboundEvent) error {
	p.cancelBodyFlushTimer(session.OutboundRouteID(event))
	var payload agentkit.MessageEndPayload
	fallbackText := ""
	if err := json.Unmarshal(event.Data, &payload); err == nil {
		fallbackText = strings.TrimSpace(assistantText(payload.Message))
	}

	st := p.streamState(session.OutboundRouteID(event))
	st.mu.Lock()
	bodyText := st.bodyText
	if bodyText == "" {
		bodyText = fallbackText
	}
	if p.useUnifiedStreamCard() {
		if bodyText != "" {
			st.bodyText = bodyText
		}
		st.mu.Unlock()
		if err := p.flushUnifiedBody(ctx, session.OutboundRouteID(event)); err != nil {
			return err
		}
		if err := p.flushUnifiedProgress(ctx, session.OutboundRouteID(event)); err != nil {
			return err
		}
		return nil
	}

	bodyHandle := st.bodyHandle
	progressHandle := st.progressHandle
	st.bodyText = ""
	st.bodyHandle = nil
	st.progressHandle = nil
	st.activeSegment = streamSegmentNone
	st.mu.Unlock()

	if bodyHandle != nil && strings.TrimSpace(bodyText) != "" {
		if err := p.finalizeBodyCard(ctx, bodyHandle, bodyText); err != nil {
			return err
		}
	}
	if progressHandle != nil {
		if err := p.finalizeProgressCard(ctx, session.OutboundRouteID(event), progressHandle); err != nil {
			return err
		}
	}

	if bodyHandle == nil && strings.TrimSpace(bodyText) != "" {
		rc, ok := p.deliveryFor(session.OutboundRouteID(event))
		if !ok {
			return nil
		}
		newHandle, err := p.SendPreviewStart(ctx, rc, buildFinalPreviewCardJSON(bodyText))
		if err != nil {
			return err
		}
		st.mu.Lock()
		st.enqueueCard(streamCardBody, newHandle)
		p.evictStreamCards(ctx, st)
		st.mu.Unlock()
		return nil
	}
	return nil
}

func (p *Platform) handleRichTurnEnd(ctx context.Context, sessionID agentkit.SessionID, endData session.TurnEndData) error {
	st := p.streamState(sessionID)
	st.mu.Lock()
	if st.startedAt.IsZero() {
		st.mu.Unlock()
		return nil
	}
	finalStatus := cardStatusDone
	switch {
	case endData.Cancelled:
		finalStatus = cardStatusCancelled
	case endData.Failed:
		finalStatus = cardStatusError
	}
	if p.useUnifiedStreamCard() {
		cardHandle := st.cardHandle
		botReplyID := botReplyMessageID(st)
		cardReactionID := st.cardReactionID
		bodyText := unifiedTurnEndBodyMarkdown(st)
		if endData.Cancelled && strings.TrimSpace(bodyText) == "" {
			bodyText = cancelledBodyText(endData.StopReason)
		}
		progressMD := p.unifiedTurnEndProgressMarkdown(st)
		if endData.Cancelled {
			progressMD = appendCancelledProgressFooter(progressMD, endData.StopReason)
		}
		st.status = finalStatus
		st.bodyText = ""
		st.lastStreamedBody = ""
		st.lastStreamedProgress = ""
		st.cardReactionID = ""
		st.cardHandle = nil
		st.mu.Unlock()
		if cardHandle != nil {
			if err := p.finalizeUnifiedStreamCard(ctx, cardHandle.(*feishuPreviewHandle), bodyText, progressMD, p.showStreamProgress()); err != nil {
				slog.Debug(p.tag()+": finalize unified card on turn end failed", "session_id", sessionID, "error", err)
			}
		}
		p.addBotReplyEndReaction(botReplyID, endData, cardReactionID)
		p.clearStream(sessionID)
		return nil
	}

	progressHandle := st.progressHandle
	bodyHandle := st.bodyHandle
	botReplyID := botReplyMessageID(st)
	cardReactionID := st.cardReactionID
	bodyText := st.bodyText
	if endData.Cancelled && strings.TrimSpace(bodyText) == "" {
		bodyText = cancelledBodyText(endData.StopReason)
	}
	st.status = finalStatus
	st.progressHandle = nil
	st.bodyHandle = nil
	st.cardReactionID = ""
	st.activeSegment = streamSegmentNone
	st.mu.Unlock()

	if bodyHandle != nil && strings.TrimSpace(bodyText) != "" {
		if err := p.finalizeBodyCard(ctx, bodyHandle, bodyText); err != nil {
			slog.Debug(p.tag()+": finalize body card on turn end failed", "session_id", sessionID, "error", err)
		}
	} else if endData.Cancelled && strings.TrimSpace(bodyText) != "" {
		rc, ok := p.deliveryFor(sessionID)
		if ok {
			if _, err := p.SendPreviewStart(ctx, rc, buildFinalPreviewCardJSON(bodyText)); err != nil {
				slog.Debug(p.tag()+": send cancelled body card failed", "session_id", sessionID, "error", err)
			}
		}
	}
	if progressHandle != nil {
		if endData.Cancelled {
			st := p.streamState(sessionID)
			st.mu.Lock()
			st.status = cardStatusCancelled
			st.mu.Unlock()
		}
		if err := p.finalizeProgressCard(ctx, sessionID, progressHandle); err != nil {
			slog.Debug(p.tag()+": finalize progress card on turn end failed", "session_id", sessionID, "error", err)
		}
	}
	p.addBotReplyEndReaction(botReplyID, endData, cardReactionID)
	p.clearStream(sessionID)
	return nil
}

func cancelledBodyText(reason string) string {
	reason = strings.TrimSpace(reason)
	if reason == "/stop" {
		return "任务已取消（/stop）"
	}
	if reason != "" && reason != "cancelled" {
		return "任务已取消（" + reason + "）"
	}
	return "任务已取消"
}

func appendCancelledProgressFooter(progressMD, reason string) string {
	footer := "> 已取消"
	reason = strings.TrimSpace(reason)
	if reason != "" && reason != "cancelled" {
		footer += "（" + reason + "）"
	}
	if strings.TrimSpace(progressMD) == "" {
		return footer
	}
	return progressMD + "\n\n---\n\n" + footer
}

func (p *Platform) finalizeProgressCard(ctx context.Context, sessionID agentkit.SessionID, handle any) error {
	st := p.streamState(sessionID)
	st.mu.Lock()
	content := p.renderProgressContent(st, false)
	st.mu.Unlock()
	if strings.TrimSpace(content) == "" || content == " " {
		return nil
	}
	return p.UpdateMessage(ctx, handle, content)
}

func (p *Platform) bootstrapReplyCard(ctx context.Context, sessionID agentkit.SessionID) error {
	if !p.useRichStream() {
		return nil
	}
	if p.useUnifiedStreamCard() {
		if _, err := p.ensureUnifiedCard(ctx, sessionID); err != nil {
			return err
		}
		return nil
	}
	if !p.showStreamProgress() {
		return nil
	}
	rc, ok := p.deliveryFor(sessionID)
	if !ok {
		return nil
	}
	st := p.streamState(sessionID)
	st.mu.Lock()
	if st.progressHandle != nil {
		st.mu.Unlock()
		return nil
	}
	st.mu.Unlock()

	content := p.renderProgressContent(st, true)
	if strings.TrimSpace(content) == "" || content == " " {
		content = buildRichCard(cardStatusThinking, "", nil, "", true, 0)
	}
	newHandle, err := p.SendPreviewStart(ctx, rc, content)
	if err != nil {
		return err
	}
	st.mu.Lock()
	st.progressHandle = newHandle
	st.enqueueCard(streamCardProgress, newHandle)
	p.evictStreamCards(ctx, st)
	st.lastProgressUpdate = time.Now()
	st.mu.Unlock()
	return nil
}

func (p *Platform) ensureUnifiedCard(ctx context.Context, sessionID agentkit.SessionID) (*feishuPreviewHandle, error) {
	st := p.streamState(sessionID)
	st.mu.Lock()
	if h, ok := st.cardHandle.(*feishuPreviewHandle); ok && h != nil {
		st.mu.Unlock()
		return h, nil
	}
	st.mu.Unlock()

	rc, ok := p.deliveryFor(sessionID)
	if !ok {
		return nil, nil
	}
	handle, err := p.createAndSendCardEntity(ctx, rc, buildUnifiedStreamingCardJSON(p.showStreamProgress()), "")
	if err != nil {
		return nil, err
	}
	handle.streaming = true
	st.mu.Lock()
	st.cardHandle = handle
	st.mu.Unlock()
	p.kickoffUnifiedCardLiveness(sessionID)
	return handle, nil
}

// kickoffUnifiedCardLiveness runs once when the unified CardKit reply card is first created:
// attach the processing reaction and write the first timestamp line without waiting for the 10s ticker.
func (p *Platform) kickoffUnifiedCardLiveness(sessionID agentkit.SessionID) {
	go p.attachCardProcessingReaction(sessionID)
	go func() {
		if !p.useUnifiedStreamCard() {
			return
		}
		if err := p.flushUnifiedStreamTimestamp(context.Background(), sessionID); err != nil && !isCardStreamingClosedError(err) {
			slog.Debug(p.tag()+": initial stream timestamp failed", "session_id", sessionID, "error", err)
		}
	}()
}

func (p *Platform) flushUnifiedProgress(ctx context.Context, sessionID agentkit.SessionID) error {
	if !p.showStreamProgress() {
		return nil
	}
	st := p.streamState(sessionID)
	st.mu.Lock()
	content := p.unifiedProgressStreamMarkdown(st, true)
	last := st.lastStreamedProgress
	st.mu.Unlock()
	if strings.TrimSpace(content) == "" {
		return nil
	}
	if content == last {
		return nil
	}
	h, err := p.ensureUnifiedCard(ctx, sessionID)
	if err != nil || h == nil {
		return err
	}
	processed := content
	if containsMarkdown(content) {
		processed = preprocessFeishuMarkdown(content)
	}
	if err := p.streamCardElementByID(ctx, h, progressStreamElementID, sanitizeMarkdownURLs(processed)); err != nil {
		return err
	}
	st.mu.Lock()
	st.lastStreamedProgress = content
	st.lastProgressUpdate = time.Now()
	st.mu.Unlock()
	return nil
}

func (p *Platform) flushUnifiedBody(ctx context.Context, sessionID agentkit.SessionID) error {
	st := p.streamState(sessionID)
	st.mu.Lock()
	bodyText := unifiedBodyStreamMarkdown(st)
	last := st.lastStreamedBody
	st.mu.Unlock()
	if strings.TrimSpace(bodyText) == "" {
		return nil
	}
	if bodyText == last {
		return nil
	}
	h, err := p.ensureUnifiedCard(ctx, sessionID)
	if err != nil || h == nil {
		return err
	}
	processed := bodyText
	if containsMarkdown(bodyText) {
		processed = preprocessFeishuMarkdown(bodyText)
	}
	if err := p.streamCardElementByID(ctx, h, bodyStreamElementID, sanitizeMarkdownURLs(processed)); err != nil {
		return err
	}
	st.mu.Lock()
	st.lastStreamedBody = bodyText
	st.lastBodyUpdate = time.Now()
	st.mu.Unlock()
	return nil
}

func countProgressToolInvocations(steps []toolStep) int {
	n := 0
	for _, step := range steps {
		switch step.Kind {
		case toolStepKindTool, toolStepKindSubagent:
			n++
		}
	}
	return n
}

func progressElapsed(st *streamState) time.Duration {
	elapsed := time.Since(st.progressStartedAt)
	if st.progressStartedAt.IsZero() {
		elapsed = time.Since(st.startedAt)
	}
	if elapsed < 0 {
		return 0
	}
	return elapsed
}

func (p *Platform) formatProgressStatusLine(toolCount int, elapsed time.Duration, streaming bool) string {
	parts := make([]string, 0, 3)
	if streaming {
		parts = append(parts, "⏱ 运行中")
	} else {
		parts = append(parts, "⏱ 用时 "+formatElapsedCN(elapsed))
	}
	if p.showToolProgress {
		parts = append(parts, fmt.Sprintf("%d 个工具", toolCount))
	}
	if streaming {
		parts = append(parts, formatElapsedCN(elapsed))
	}
	return strings.Join(parts, " · ")
}

func (p *Platform) renderProgressMarkdown(st *streamState, streaming bool) string {
	body := p.renderProgressBody(st)
	if strings.TrimSpace(body) == "" {
		if !streaming {
			return "> " + p.formatProgressStatusLine(countProgressToolInvocations(st.steps), progressElapsed(st), false)
		}
		return ""
	}
	if streaming {
		return body
	}
	footer := "> " + p.formatProgressStatusLine(countProgressToolInvocations(st.steps), progressElapsed(st), false)
	return body + progressMarkdownFooterSeparator + footer
}

func (p *Platform) renderProgressBody(st *streamState) string {
	var b strings.Builder
	if p.showThinking && strings.TrimSpace(st.thinking) != "" {
		b.WriteString("> ")
		b.WriteString(strings.TrimSpace(st.thinking))
		b.WriteString("\n\n")
	}
	steps := st.steps
	if len(steps) == 0 {
		return strings.TrimSpace(b.String())
	}
	visible := steps
	truncated := false
	stepLimit := maxRecentProgressSteps
	if p.useUnifiedStreamCard() {
		stepLimit = 0
	}
	if stepLimit > 0 && len(steps) > stepLimit {
		visible = steps[len(steps)-stepLimit:]
		truncated = true
	}
	for _, step := range visible {
		switch step.Kind {
		case toolStepKindTool:
			b.WriteString("- **")
			b.WriteString(step.Name)
			b.WriteString("**")
			if summary := strings.TrimSpace(step.Summary); summary != "" && summary != step.Name {
				b.WriteString(" ")
				b.WriteString(summary)
			}
			b.WriteString("\n")
		case toolStepKindSubagent:
			label := subagentToolLabel(step.Name)
			b.WriteString("- **")
			b.WriteString(label)
			b.WriteString("**")
			if step.Done {
				if summary := strings.TrimSpace(step.Result); summary != "" {
					b.WriteString(" ")
					b.WriteString(summary)
				} else if summary := strings.TrimSpace(step.Summary); summary != "" {
					b.WriteString(" ")
					b.WriteString(summary)
				}
			} else if summary := strings.TrimSpace(step.Summary); summary != "" {
				b.WriteString(" ")
				b.WriteString(summary)
			}
			b.WriteString("\n")
		case toolStepKindToolResult:
			b.WriteString("  - ")
			if result := strings.TrimSpace(step.Result); result != "" {
				b.WriteString(result)
			} else {
				b.WriteString(strings.TrimSpace(step.Summary))
			}
			b.WriteString("\n")
		}
	}
	if truncated {
		b.WriteString("\n_仅显示最近更新_\n")
	}
	return strings.TrimSpace(b.String())
}

func (p *Platform) flushProgressCard(ctx context.Context, sessionID agentkit.SessionID, streaming bool) error {
	rc, ok := p.deliveryFor(sessionID)
	if !ok {
		return nil
	}

	st := p.streamState(sessionID)
	st.mu.Lock()
	content := p.renderProgressContent(st, streaming)
	handle := st.progressHandle
	hasProgress := len(st.steps) > 0 || strings.TrimSpace(st.thinking) != ""
	empty := strings.TrimSpace(content) == "" || content == " "
	st.mu.Unlock()

	if empty && !hasProgress {
		return nil
	}

	if handle == nil {
		newHandle, err := p.SendPreviewStart(ctx, rc, content)
		if err != nil {
			return err
		}
		st.mu.Lock()
		st.progressHandle = newHandle
		st.enqueueCard(streamCardProgress, newHandle)
		p.evictStreamCards(ctx, st)
		st.lastProgressUpdate = time.Now()
		st.mu.Unlock()
		return nil
	}
	if err := p.UpdateMessage(ctx, handle, content); err != nil {
		return err
	}
	st.mu.Lock()
	st.lastProgressUpdate = time.Now()
	st.mu.Unlock()
	return nil
}

func (p *Platform) flushBodyCard(ctx context.Context, sessionID agentkit.SessionID, streaming bool) error {
	rc, ok := p.deliveryFor(sessionID)
	if !ok {
		return nil
	}

	st := p.streamState(sessionID)
	st.mu.Lock()
	bodyText := st.bodyText
	handle := st.bodyHandle
	st.mu.Unlock()

	if strings.TrimSpace(bodyText) == "" {
		return nil
	}

	content := bodyText
	if !streaming {
		content = buildFinalPreviewCardJSON(bodyText)
	}

	if handle == nil {
		newHandle, err := p.SendPreviewStart(ctx, rc, content)
		if err != nil {
			return err
		}
		st.mu.Lock()
		st.bodyHandle = newHandle
		st.enqueueCard(streamCardBody, newHandle)
		p.evictStreamCards(ctx, st)
		st.lastBodyUpdate = time.Now()
		st.mu.Unlock()
		return nil
	}
	if err := p.UpdateMessage(ctx, handle, content); err != nil {
		return err
	}
	st.mu.Lock()
	st.lastBodyUpdate = time.Now()
	st.mu.Unlock()
	return nil
}

func (p *Platform) renderProgressContent(st *streamState, streaming bool) string {
	elapsed := time.Since(st.progressStartedAt)
	if st.progressStartedAt.IsZero() {
		elapsed = time.Since(st.startedAt)
	}
	if p.progressStyle == "compact" {
		return p.renderCompactProgressCard(st, streaming)
	}
	steps := p.renderRichSteps(st)
	if len(steps) == 0 && !streaming {
		return " "
	}
	return buildRichCard(st.status, "", steps, "", streaming, elapsed)
}

func (p *Platform) renderRichSteps(st *streamState) []toolStep {
	steps := make([]toolStep, 0, len(st.steps)+1)
	if p.showThinking && strings.TrimSpace(st.thinking) != "" {
		steps = append(steps, toolStep{
			Kind:    toolStepKindThinking,
			Summary: strings.TrimSpace(st.thinking),
		})
	}
	steps = append(steps, st.steps...)
	return steps
}

func (p *Platform) renderCompactProgressCard(st *streamState, streaming bool) string {
	state := common.ProgressCardStateRunning
	if !streaming {
		state = common.ProgressCardStateCompleted
	}
	progressItems := make([]common.ProgressCardEntry, 0, len(st.steps)+1)
	if p.showThinking && strings.TrimSpace(st.thinking) != "" {
		progressItems = append(progressItems, common.ProgressCardEntry{
			Kind: common.ProgressEntryThinking,
			Text: strings.TrimSpace(st.thinking),
		})
	}
	for _, step := range st.steps {
		switch step.Kind {
		case toolStepKindTool, toolStepKindSubagent:
			if step.Kind == toolStepKindSubagent && step.Done {
				success := true
				if step.Success != nil {
					success = *step.Success
				}
				progressItems = append(progressItems, common.ProgressCardEntry{
					Kind:    common.ProgressEntryToolResult,
					Tool:    subagentToolLabel(step.Name),
					Text:    step.Result,
					Status:  step.Status,
					Success: &success,
				})
				continue
			}
			toolLabel := step.Name
			if step.Kind == toolStepKindSubagent {
				toolLabel = subagentToolLabel(step.Name)
			}
			progressItems = append(progressItems, common.ProgressCardEntry{
				Kind: common.ProgressEntryToolUse,
				Tool: toolLabel,
				Text: step.Summary,
			})
		case toolStepKindToolResult:
			success := true
			if step.Success != nil {
				success = *step.Success
			}
			progressItems = append(progressItems, common.ProgressCardEntry{
				Kind:    common.ProgressEntryToolResult,
				Tool:    step.Name,
				Text:    step.Result,
				Status:  step.Status,
				Success: &success,
			})
		}
	}
	truncated := len(progressItems) > maxRecentProgressSteps
	if truncated {
		progressItems = progressItems[len(progressItems)-maxRecentProgressSteps:]
	}
	if len(progressItems) == 0 {
		return buildCardJSON(" ")
	}
	payload := &common.ProgressCardPayload{
		Version:   1,
		State:     state,
		Items:     progressItems,
		Truncated: truncated,
	}
	raw, _ := json.Marshal(payload)
	return common.ProgressCardPayloadPrefix + string(raw)
}

func truncateRunes(s string, max int) string {
	s = strings.TrimSpace(s)
	if max <= 0 {
		return s
	}
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max]) + "..."
}
