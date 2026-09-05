package feishu

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"time"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/platform/common"
	"github.com/lengzhao/agentkit/runtime/session"
)

const maxToolSummaryRunes = 180
const maxRecentProgressSteps = 2
const progressHeartbeatInterval = 5 * time.Second

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
	ticker := time.NewTicker(progressHeartbeatInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.tickProgressHeartbeat(sessionID)
		}
	}
}

func shouldHeartbeatFlush(st *streamState) bool {
	st.mu.Lock()
	defer st.mu.Unlock()
	return !st.startedAt.IsZero() &&
		st.progressHandle != nil &&
		st.bodyHandle == nil &&
		st.status != cardStatusDone &&
		st.status != cardStatusError
}

func (p *Platform) tickProgressHeartbeat(sessionID agentkit.SessionID) {
	raw, ok := p.streams.Load(sessionID)
	if !ok {
		return
	}
	st := raw.(*streamState)
	if !shouldHeartbeatFlush(st) {
		return
	}
	if err := p.flushProgressCard(context.Background(), sessionID, true); err != nil {
		slog.Debug(p.tag()+": progress heartbeat flush failed", "session_id", sessionID, "error", err)
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

func (p *Platform) handleRichStreamMessageStart(_ context.Context, sessionID agentkit.SessionID) error {
	p.cancelBodyFlushTimer(sessionID)
	st := p.richStreamState(sessionID)
	st.mu.Lock()
	st.thinking = ""
	st.steps = nil
	st.toolStepIdx = make(map[int]int)
	st.bodyText = ""
	st.bodyHandle = nil
	st.progressHandle = nil
	st.status = cardStatusThinking
	st.progressStartedAt = time.Now()
	st.mu.Unlock()
	return nil
}

func (p *Platform) handleRichStreamUpdate(ctx context.Context, sessionID agentkit.SessionID, ame agentkit.AssistantMessageEvent) error {
	if ame.Type == agentkit.AssistantEventTextDelta {
		return p.handleRichBodyDelta(ctx, sessionID, ame.Delta)
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
	delay := streamFlushDelay(st.lastBodyUpdate, streamUpdateInterval)
	sid := sessionID
	st.bodyFlushTimer = time.AfterFunc(delay, func() {
		st.mu.Lock()
		stopStreamTimer(&st.bodyFlushTimer)
		st.mu.Unlock()
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
	delay := streamFlushDelay(st.lastUpdate, streamUpdateInterval)
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

	st := p.richStreamState(sessionID)
	st.mu.Lock()
	st.bodyText += delta
	st.status = cardStatusWorking
	shouldFlushNow := st.bodyHandle == nil || time.Since(st.lastBodyUpdate) >= streamUpdateInterval
	st.mu.Unlock()
	if shouldFlushNow {
		p.cancelBodyFlushTimer(sessionID)
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
		if st.cards[0].Kind != streamCardProgress {
			break
		}
		handle := st.cards[0].Handle
		if err := p.DeletePreviewMessage(ctx, handle); err != nil {
			slog.Debug(p.tag()+": evict progress card failed", "error", err)
		}
		st.clearCardRef(handle)
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
	st := p.richStreamState(session.OutboundRouteID(event))
	st.mu.Lock()
	changed := p.applyToolResult(st, result)
	shouldFlush := changed && (st.progressHandle == nil || time.Since(st.lastProgressUpdate) >= streamUpdateInterval)
	st.mu.Unlock()
	if !shouldFlush {
		return nil
	}
	return p.flushProgressCard(ctx, session.OutboundRouteID(event), true)
}

func (p *Platform) handleRichSubagentEvent(ctx context.Context, event agentkit.OutboundEvent) error {
	st := p.richStreamState(session.OutboundRouteID(event))
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
	return p.flushProgressCard(ctx, session.OutboundRouteID(event), true)
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
	bodyHandle := st.bodyHandle
	progressHandle := st.progressHandle
	st.bodyText = ""
	st.bodyHandle = nil
	st.progressHandle = nil
	st.mu.Unlock()

	if bodyHandle != nil && strings.TrimSpace(bodyText) != "" {
		if err := p.UpdateMessage(ctx, bodyHandle, buildFinalPreviewCardJSON(bodyText)); err != nil {
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

func (p *Platform) handleRichTurnEnd(ctx context.Context, sessionID agentkit.SessionID) error {
	st := p.streamState(sessionID)
	st.mu.Lock()
	if st.startedAt.IsZero() {
		st.mu.Unlock()
		return nil
	}
	progressHandle := st.progressHandle
	bodyHandle := st.bodyHandle
	bodyText := st.bodyText
	st.status = cardStatusDone
	st.progressHandle = nil
	st.bodyHandle = nil
	st.mu.Unlock()

	if bodyHandle != nil && strings.TrimSpace(bodyText) != "" {
		if err := p.UpdateMessage(ctx, bodyHandle, buildFinalPreviewCardJSON(bodyText)); err != nil {
			slog.Debug(p.tag()+": finalize body card on turn end failed", "session_id", sessionID, "error", err)
		}
	}
	if progressHandle != nil {
		if err := p.finalizeProgressCard(ctx, sessionID, progressHandle); err != nil {
			slog.Debug(p.tag()+": finalize progress card on turn end failed", "session_id", sessionID, "error", err)
		}
	}
	p.clearStream(sessionID)
	return nil
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
		p.removePriorProgressCards(ctx, st, newHandle)
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
