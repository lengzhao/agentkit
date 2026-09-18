package feishu

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/rctx"
	"github.com/lengzhao/agentkit/runtime/session/sessevents"
)

const maxToolSummaryRunes = 180
const maxRecentProgressSteps = 2

const progressMarkdownFooterSeparator = "\n\n---\n\n"
const richCardBodySegmentSeparator = "\n\n"
const outboundStreamReplySuffix = ":reply:"

// outboundStreamKey isolates in-flight card state per trigger message (Route.ReplyTo).
func outboundStreamKey(event agentkit.OutboundEvent) agentkit.SessionID {
	delivery := rctx.OutboundRouteID(event)
	replyTo := strings.TrimSpace(rctx.RouteReplyTo(event.Route))
	if replyTo == "" {
		return delivery
	}
	return agentkit.SessionID(string(delivery) + outboundStreamReplySuffix + replyTo)
}

func deliveryFromStreamKey(streamKey agentkit.SessionID) agentkit.SessionID {
	s := string(streamKey)
	if i := strings.LastIndex(s, outboundStreamReplySuffix); i >= 0 {
		return agentkit.SessionID(s[:i])
	}
	return streamKey
}

func replyToFromStreamKey(streamKey agentkit.SessionID) string {
	s := string(streamKey)
	if i := strings.LastIndex(s, outboundStreamReplySuffix); i >= 0 {
		return s[i+len(outboundStreamReplySuffix):]
	}
	return ""
}

func (p *Platform) replyContextForStreamKey(streamKey agentkit.SessionID) (replyContext, bool) {
	delivery := deliveryFromStreamKey(streamKey)
	rc, ok := p.deliveryForSend(delivery)
	if !ok {
		return replyContext{}, false
	}
	if replyTo := replyToFromStreamKey(streamKey); replyTo != "" {
		rc.messageID = replyTo
	}
	return rc, true
}

func (st *streamState) commitInflightBodyText() {
	part := strings.TrimSpace(st.bodyText)
	if part == "" {
		st.bodyText = ""
		return
	}
	if prior := strings.TrimSpace(st.committedBodyText); prior != "" {
		st.committedBodyText = prior + richCardBodySegmentSeparator + part
	} else {
		st.committedBodyText = part
	}
	st.bodyText = ""
}

func (st *streamState) richCardDisplayBody() string {
	committed := strings.TrimSpace(st.committedBodyText)
	inflight := st.bodyText
	if committed == "" {
		return inflight
	}
	if strings.TrimSpace(inflight) == "" {
		return st.committedBodyText
	}
	return st.committedBodyText + richCardBodySegmentSeparator + inflight
}

func subagentToolLabel(agent string) string {
	return "子Agent:" + strings.TrimSpace(agent)
}

func (p *Platform) useRichStream() bool {
	return p.progressStyle == "card"
}

func (p *Platform) bumpRichCardPanel(st *streamState) {
	st.richCardPanelVersion++
}

func (p *Platform) canRichCardStreamBody(st *streamState, handle any) bool {
	if handle == nil {
		return false
	}
	h, ok := handle.(*feishuPreviewHandle)
	if !ok || strings.TrimSpace(h.cardID) == "" {
		return false
	}
	return st.richCardPanelVersion == st.richCardFlushedPanelVersion
}

func streamBodyMarkdownForCardKit(body string) string {
	processed := body
	if containsMarkdown(body) {
		processed = preprocessFeishuMarkdown(body)
	}
	return sanitizeMarkdownURLs(processed)
}

func (p *Platform) flushRichCard(ctx context.Context, streamKey agentkit.SessionID, streaming bool) error {
	rc, ok := p.replyContextForStreamKey(streamKey)
	if !ok {
		return nil
	}

	st := p.streamState(streamKey)
	st.lock()
	status := st.status
	if status == "" {
		status = cardStatusThinking
	}
	body := st.richCardDisplayBody()
	if strings.TrimSpace(body) != "" {
		st.finalizedBodyText = body
	}
	displaySteps := p.renderRichSteps(st)
	handle := st.progressHandle
	elapsed := progressElapsed(st)
	panelVer := st.richCardPanelVersion
	st.unlock()

	content := buildRichCard(status, "", displaySteps, body, streaming, elapsed)
	if strings.TrimSpace(content) == "" || content == " " {
		return nil
	}

	if handle == nil {
		newHandle, err := p.SendPreviewStart(ctx, rc, content)
		if err != nil {
			return err
		}
		st.lock()
		st.progressHandle = newHandle
		st.enqueueCard(streamCardProgress, newHandle)
		evicted := p.evictStreamCardsLocked(st)
		st.lastProgressUpdate = time.Now()
		st.richCardFlushedPanelVersion = panelVer
		st.lastRichCardBodyStreamRunes = len([]rune(body))
		st.unlock()
		p.deleteEvictedProgressCards(ctx, evicted)
		return nil
	}

	if streaming && p.canRichCardStreamBody(st, handle) {
		h := handle.(*feishuPreviewHandle)
		streamText := streamBodyMarkdownForCardKit(body)
		if strings.TrimSpace(streamText) != "" {
			streamErr := p.streamCardElementByID(ctx, h, richCardMainTextElementID, streamText)
			if streamErr == nil {
				st.lock()
				st.lastProgressUpdate = time.Now()
				st.lastRichCardBodyStreamRunes = len([]rune(body))
				st.unlock()
				return nil
			}
			slog.Debug(p.tag()+": rich card body stream failed, falling back to full patch", "session_id", streamKey, "error", streamErr)
		}
	}

	var err error
	if streaming {
		err = p.UpdateMessage(ctx, handle, content)
	} else {
		err = p.patchRichCard(ctx, handle, content)
	}
	if err != nil {
		return err
	}
	st.lock()
	st.lastProgressUpdate = time.Now()
	st.richCardFlushedPanelVersion = panelVer
	st.lastRichCardBodyStreamRunes = len([]rune(body))
	if !streaming && len(displaySteps) > 0 {
		st.finalizedSteps = append([]toolStep(nil), displaySteps...)
	}
	st.unlock()
	return nil
}

// maybeFlushRichCard applies streamUpdateInterval throttling but always schedules a
// deferred flush so the latest in-memory steps/body are delivered (see message/turn end).
func (p *Platform) richCardPatchThrottle(st *streamState) time.Duration {
	if p.canRichCardStreamBody(st, st.progressHandle) {
		return richCardBodyStreamInterval
	}
	return richCardFullPatchInterval
}

func (p *Platform) maybeFlushRichCard(ctx context.Context, sessionID agentkit.SessionID, changed bool) error {
	if !changed {
		return nil
	}
	st := p.streamState(sessionID)
	st.lock()
	interval := p.richCardPatchThrottle(st)
	shouldNow := st.progressHandle == nil || time.Since(st.lastProgressUpdate) >= interval
	st.unlock()
	if shouldNow {
		p.cancelBodyFlushTimer(sessionID)
		return p.flushRichCard(ctx, sessionID, true)
	}
	p.scheduleBodyFlush(sessionID)
	return nil
}

func (p *Platform) richStreamState(sessionID agentkit.SessionID) *streamState {
	st := p.streamState(sessionID)
	st.lock()
	defer st.unlock()
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

func (p *Platform) handleRichStreamMessageStart(ctx context.Context, streamKey agentkit.SessionID) error {
	p.cancelBodyFlushTimer(streamKey)
	st := p.richStreamState(streamKey)
	st.lock()
	needFlush := false
	if strings.TrimSpace(st.bodyText) != "" {
		st.commitInflightBodyText()
		needFlush = true
	} else {
		st.bodyText = ""
	}
	st.toolStepIdx = make(map[int]int)
	if st.status == "" || st.status == cardStatusThinking {
		st.status = cardStatusWorking
	}
	st.unlock()
	if needFlush {
		return p.flushRichCard(ctx, streamKey, true)
	}
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

func (p *Platform) handleRichStreamUpdate(ctx context.Context, sessionID agentkit.SessionID, event agentkit.OutboundEvent, ame agentkit.AssistantMessageEvent) error {
	if handled, err := p.handleAsyncSubagentStreamUpdate(ctx, sessionID, event, ame); handled {
		return err
	}
	seg := streamSegmentOfAssistantEvent(p, ame)
	if seg == streamSegmentBody {
		return p.handleRichBodyDelta(ctx, sessionID, ame.Delta)
	}
	if seg == streamSegmentNone {
		return nil
	}
	st := p.richStreamState(sessionID)
	st.lock()
	changed := p.applyRichStreamEvent(st, ame)
	st.unlock()
	return p.maybeFlushRichCard(ctx, sessionID, changed)
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
	st.lock()
	stopStreamTimer(&st.bodyFlushTimer)
	st.unlock()
}

func (p *Platform) cancelLegacyFlushTimer(sessionID agentkit.SessionID) {
	st := p.streamState(sessionID)
	st.lock()
	stopStreamTimer(&st.legacyFlushTimer)
	st.unlock()
}

func (p *Platform) scheduleBodyFlush(sessionID agentkit.SessionID) {
	st := p.streamState(sessionID)
	st.lock()
	if st.bodyFlushTimer != nil {
		st.unlock()
		return
	}
	lastUpdate := st.lastProgressUpdate
	interval := p.richCardPatchThrottle(st)
	delay := streamFlushDelay(lastUpdate, interval)
	sid := sessionID
	st.bodyFlushTimer = time.AfterFunc(delay, func() {
		st.lock()
		stopStreamTimer(&st.bodyFlushTimer)
		st.unlock()
		cur := p.streamState(sid)
		cur.lock()
		active := cur.progressHandle != nil ||
			len(cur.steps) > 0 ||
			strings.TrimSpace(cur.thinking) != "" ||
			strings.TrimSpace(cur.richCardDisplayBody()) != ""
		cur.unlock()
		if !active {
			return
		}
		if err := p.flushRichCard(context.Background(), sid, true); err != nil {
			slog.Debug(p.tag()+": debounced patch rich card flush failed", "session_id", sid, "error", err)
		}
	})
	st.unlock()
}

func (p *Platform) scheduleLegacyFlush(sessionID agentkit.SessionID) {
	st := p.streamState(sessionID)
	st.lock()
	if st.legacyFlushTimer != nil {
		st.unlock()
		return
	}
	delay := streamFlushDelay(st.lastUpdate, p.bodyStreamInterval())
	sid := sessionID
	st.legacyFlushTimer = time.AfterFunc(delay, func() {
		st.lock()
		stopStreamTimer(&st.legacyFlushTimer)
		st.unlock()
		st = p.streamState(sid)
		st.lock()
		text := st.accumulated
		st.unlock()
		if strings.TrimSpace(text) == "" {
			return
		}
		if err := p.flushStream(context.Background(), sid, text); err != nil {
			slog.Debug(p.tag()+": debounced legacy flush failed", "session_id", sid, "error", err)
		}
	})
	st.unlock()
}

func (p *Platform) handleRichBodyDelta(ctx context.Context, sessionID agentkit.SessionID, delta string) error {
	if delta == "" {
		return nil
	}

	st := p.richStreamState(sessionID)
	st.lock()
	prevStatus := st.status
	st.bodyText += delta
	displayBody := st.richCardDisplayBody()
	st.finalizedBodyText = displayBody
	st.status = cardStatusWorking
	if prevStatus != cardStatusWorking {
		p.bumpRichCardPanel(st)
	}
	interval := p.richCardPatchThrottle(st)
	elapsed := time.Since(st.lastProgressUpdate)
	bodyGrowth := len([]rune(displayBody)) - st.lastRichCardBodyStreamRunes
	shouldFlushNow := st.progressHandle == nil ||
		elapsed >= interval ||
		(p.canRichCardStreamBody(st, st.progressHandle) && bodyGrowth >= richCardBodyStreamMinRunes)
	st.unlock()
	if shouldFlushNow {
		p.cancelBodyFlushTimer(sessionID)
		return p.flushRichCard(ctx, sessionID, true)
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
}

func (p *Platform) removePriorProgressCards(ctx context.Context, st *streamState, keep any) {
	remaining := make([]streamCard, 0, len(st.cards))
	var toDelete []any
	for _, card := range st.cards {
		if card.Kind != streamCardProgress || card.Handle == keep {
			remaining = append(remaining, card)
			continue
		}
		toDelete = append(toDelete, card.Handle)
		st.clearCardRef(card.Handle)
	}
	st.cards = remaining
	p.deleteEvictedProgressCards(ctx, toDelete)
}

func (p *Platform) evictStreamCards(ctx context.Context, st *streamState) {
	evicted := p.evictStreamCardsLocked(st)
	p.deleteEvictedProgressCards(ctx, evicted)
}

func (p *Platform) evictStreamCardsLocked(st *streamState) []any {
	var toDelete []any
	for len(st.cards) > maxStreamCards {
		oldest := st.cards[0]
		if oldest.Kind == streamCardProgress {
			toDelete = append(toDelete, oldest.Handle)
		}
		st.clearCardRef(oldest.Handle)
		st.cards = st.cards[1:]
	}
	return toDelete
}

func (p *Platform) deleteEvictedProgressCards(ctx context.Context, handles []any) {
	for _, handle := range handles {
		if handle == nil {
			continue
		}
		if err := p.DeletePreviewMessage(ctx, handle); err != nil {
			slog.Debug(p.tag()+": evict progress card failed", "error", err)
		}
	}
}

func toolCallID(ame agentkit.AssistantMessageEvent) string {
	callID := strings.TrimSpace(ame.ID)
	if callID == "" && ame.ToolCall != nil {
		callID = string(ame.ToolCall.ID)
	}
	return callID
}

// toolStepIndexForCall resolves a tool step for toolcall_end. ACP agents reuse
// the same ContentIndex for every tool call, so CallID takes precedence.
func (p *Platform) toolStepIndexForCall(st *streamState, ame agentkit.AssistantMessageEvent) (int, bool) {
	if callID := toolCallID(ame); callID != "" {
		for i := range st.steps {
			step := st.steps[i]
			if step.Kind == toolStepKindTool && step.CallID == callID {
				return i, true
			}
		}
	}
	idx, ok := st.toolStepIdx[ame.ContentIndex]
	return idx, ok
}

func (p *Platform) applyRichStreamEvent(st *streamState, ame agentkit.AssistantMessageEvent) bool {
	switch ame.Type {
	case agentkit.AssistantEventThinkingDelta:
		if !p.showThinking || ame.Delta == "" {
			return false
		}
		st.thinking += ame.Delta
		st.status = cardStatusThinking
		p.bumpRichCardPanel(st)
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
		p.bumpRichCardPanel(st)
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
		p.bumpRichCardPanel(st)
		return true
	case agentkit.AssistantEventToolCallEnd:
		if !p.showToolProgress {
			return false
		}
		idx, ok := p.toolStepIndexForCall(st, ame)
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
		p.bumpRichCardPanel(st)
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
		if strings.TrimSpace(result.Audit["status"]) == "failed" {
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
	p.bumpRichCardPanel(st)
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
	streamKey := outboundStreamKey(event)
	if handled, err := p.handleAsyncSubagentToolResult(ctx, streamKey, event, result); handled {
		return err
	}
	st := p.richStreamState(streamKey)
	st.lock()
	changed := p.applyToolResult(st, result)
	st.unlock()
	return p.maybeFlushRichCard(ctx, streamKey, changed)
}

func (p *Platform) handleRichSubagentEvent(ctx context.Context, event agentkit.OutboundEvent) error {
	if !p.showToolProgress {
		return nil
	}
	streamKey := outboundStreamKey(event)
	if event.Type == agentkit.EventSubagentStart {
		var data sessevents.SubagentStartData
		if err := json.Unmarshal(event.Data, &data); err != nil {
			return err
		}
		if data.Async && p.asyncSubagentCardEnabled() {
			return p.handleAsyncSubagentStart(ctx, streamKey, event.AgentID, data)
		}
	}
	if event.Type == agentkit.EventSubagentEnd {
		var data sessevents.SubagentEndData
		if err := json.Unmarshal(event.Data, &data); err != nil {
			return err
		}
		if p.asyncSubagentCardEnabled() {
			jobID := strings.TrimSpace(data.JobID)
			if jobID == "" {
				jobID = strings.TrimSpace(data.Session)
			}
			if jobID != "" {
				if _, ok := p.asyncSubagentByJob.Load(jobID); ok {
					return p.handleAsyncSubagentEnd(ctx, data)
				}
			}
		}
	}
	st := p.richStreamState(streamKey)
	st.lock()
	changed := false
	switch event.Type {
	case agentkit.EventSubagentStart:
		var data sessevents.SubagentStartData
		if err := json.Unmarshal(event.Data, &data); err != nil {
			st.unlock()
			return err
		}
		appendSubagentStartStep(p, st, data.Agent, data.Task)
		changed = true
	case agentkit.EventSubagentEnd:
		var data sessevents.SubagentEndData
		if err := json.Unmarshal(event.Data, &data); err != nil {
			st.unlock()
			return err
		}
		appendSubagentEndStep(p, st, data)
		changed = true
	}
	st.unlock()
	return p.maybeFlushRichCard(ctx, streamKey, changed)
}

func subagentDisplayName(agent string) string {
	agent = strings.TrimSpace(agent)
	if agent == "" {
		return "subagent"
	}
	return agent
}

func appendSubagentStartStep(p *Platform, st *streamState, agent, task string) {
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
	p.bumpRichCardPanel(st)
}

func appendSubagentEndStep(p *Platform, st *streamState, data sessevents.SubagentEndData) {
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
	p.bumpRichCardPanel(st)
}

func (p *Platform) handleRichStreamMessageEnd(ctx context.Context, event agentkit.OutboundEvent) error {
	streamKey := outboundStreamKey(event)
	p.cancelBodyFlushTimer(streamKey)
	var payload agentkit.MessageEndPayload
	fallbackText := ""
	if err := json.Unmarshal(event.Data, &payload); err == nil {
		fallbackText = strings.TrimSpace(assistantText(payload.Message))
	}

	st := p.streamState(streamKey)
	st.lock()
	bodyText := st.bodyText
	if bodyText == "" {
		bodyText = fallbackText
	}
	if bodyText != "" {
		st.bodyText = bodyText
	}
	st.finalizedBodyText = st.richCardDisplayBody()
	st.unlock()
	// 整卡落盘（关闭 streaming、去掉进行中页脚），避免仅流式 main_text 时页脚残留在旧 JSON。
	return p.flushRichCard(ctx, streamKey, false)
}

// finalizeRichTurnEndAsync patches the CardKit entity without blocking turn/end teardown.
func (p *Platform) finalizeRichTurnEndAsync(
	ctx context.Context,
	sessionID agentkit.SessionID,
	handle any,
	content string,
	botReplyID string,
	endData sessevents.TurnEndData,
) {
	parent := context.WithoutCancel(ctx)
	go func() {
		if handle != nil {
			if err := p.patchRichCard(parent, handle, content); err != nil {
				slog.Warn(p.tag()+": finalize patch rich card on turn end failed", "session_id", sessionID, "error", err)
			}
		} else if rc, ok := p.replyContextForStreamKey(sessionID); ok {
			if _, err := p.SendPreviewStart(parent, rc, content); err != nil {
				slog.Debug(p.tag()+": send final rich card on turn end failed", "session_id", sessionID, "error", err)
			}
		}
		p.addBotReplyEndReaction(botReplyID, endData, "")
	}()
}

func (p *Platform) handleRichTurnEnd(ctx context.Context, sessionID agentkit.SessionID, endData sessevents.TurnEndData) error {
	st := p.streamState(sessionID)
	st.lock()
	hasHandle := st.progressHandle != nil
	if st.startedAt.IsZero() && !hasHandle {
		st.unlock()
		return nil
	}
	finalStatus := cardStatusDone
	switch {
	case endData.Cancelled:
		finalStatus = cardStatusCancelled
	case endData.Failed:
		finalStatus = cardStatusError
	}
	stopStreamTimer(&st.bodyFlushTimer)
	handle := st.progressHandle
	botReplyID := botReplyMessageID(st)
	bodyText := strings.TrimSpace(st.richCardDisplayBody())
	if bodyText == "" {
		bodyText = strings.TrimSpace(st.finalizedBodyText)
	}
	if endData.Cancelled && bodyText == "" {
		bodyText = cancelledBodyText(endData.StopReason)
	}
	displaySteps := p.renderRichSteps(st)
	if len(displaySteps) == 0 && len(st.finalizedSteps) > 0 {
		displaySteps = append([]toolStep(nil), st.finalizedSteps...)
	}
	elapsed := progressElapsed(st)
	st.status = finalStatus
	st.bodyText = ""
	st.progressHandle = nil
	st.unlock()

	content := buildRichCard(finalStatus, "", displaySteps, bodyText, false, elapsed)
	p.clearStream(sessionID)
	if strings.TrimSpace(content) != "" && content != " " {
		p.finalizeRichTurnEndAsync(ctx, sessionID, handle, content, botReplyID, endData)
	} else {
		p.addBotReplyEndReaction(botReplyID, endData, "")
	}
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

func (p *Platform) bootstrapReplyCard(ctx context.Context, streamKey agentkit.SessionID) error {
	if !p.useRichStream() {
		return nil
	}
	return p.flushRichCard(ctx, streamKey, true)
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
