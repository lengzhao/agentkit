package feishu

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/lengzhao/agentkit"
	capsession "github.com/lengzhao/agentkit/cap/session"
	"github.com/lengzhao/agentkit/runtime/rctx"
	"github.com/lengzhao/agentkit/runtime/session/sessevents"
)

const maxToolSummaryRunes = 180
const maxRecentProgressSteps = 2

const progressMarkdownFooterSeparator = "\n\n---\n\n"
const richCardBodySegmentSeparator = "\n\n"
const outboundStreamReplySuffix = ":reply:"

// outboundStreamAsyncSuffix 把后台委派 job 的进度卡挂到 streams 的独立 key 下，
// 与父 turn 回复卡共用同一套 flush / 心跳 / 重建逻辑。
const outboundStreamAsyncSuffix = ":async:"

func asyncStreamKey(streamKey agentkit.SessionID, jobID string) agentkit.SessionID {
	return agentkit.SessionID(string(streamKey) + outboundStreamAsyncSuffix + jobID)
}

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
	if i := strings.LastIndex(s, outboundStreamAsyncSuffix); i >= 0 {
		s = s[:i]
	}
	if i := strings.LastIndex(s, outboundStreamReplySuffix); i >= 0 {
		return agentkit.SessionID(s[:i])
	}
	return agentkit.SessionID(s)
}

func replyToFromStreamKey(streamKey agentkit.SessionID) string {
	s := string(streamKey)
	if i := strings.LastIndex(s, outboundStreamAsyncSuffix); i >= 0 {
		s = s[:i]
	}
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

func (p *Platform) bumpRichCardPanel(st *streamState) {
	st.richCardPanelVersion++
}

func (p *Platform) canRichCardStreamBody(st *streamState, handle any) bool {
	if handle == nil || st.bodyStreamClosed {
		return false
	}
	h, ok := handle.(*feishuPreviewHandle)
	if !ok || strings.TrimSpace(h.cardID) == "" {
		return false
	}
	return st.richCardPanelVersion == st.richCardFlushedPanelVersion
}

// isStreamClosedError 识别 CardKit 300309「streaming mode is closed」：整卡更新
// （UpdateMessage/patch）会关闭实体的流式模式，之后再尝试元素流式必失败。
func isStreamClosedError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "300309") || strings.Contains(msg, "streaming mode is closed")
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
	var body string
	if st.bodyFn != nil {
		body = st.bodyFn(streaming)
	} else {
		body = st.richCardDisplayBody()
		if strings.TrimSpace(body) != "" {
			st.finalizedBodyText = body
		}
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
		newHandle, err := p.createProgressCard(ctx, streamKey, rc, content)
		if err != nil {
			// 卡片创建失败不应中断 agent turn；最终答复仍走普通消息路径。
			if !errors.Is(err, errRichCardCreateCooldown) {
				slog.Warn(p.tag()+": rich card create failed, continue without progress card", "session_id", streamKey, "error", err)
			}
			return nil
		}
		st.lock()
		st.progressHandle = newHandle
		st.bodyStreamClosed = false // 新实体流式模式重新可用
		st.enqueueCard(streamCardProgress, newHandle)
		evicted := p.evictStreamCardsLocked(st)
		st.lastProgressUpdate = time.Now()
		st.richCardFlushedPanelVersion = panelVer
		st.lastRichCardBodyStreamRunes = len([]rune(body))
		st.unlock()
		p.deleteEvictedProgressCards(ctx, evicted)
		p.scheduleRichCardKeepalive(streamKey)
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
				p.scheduleRichCardKeepalive(streamKey)
				return nil
			}
			if isStreamClosedError(streamErr) {
				// 流式模式已被整卡更新关闭：标记后本卡不再尝试元素流式。
				st.lock()
				st.bodyStreamClosed = true
				st.unlock()
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
		// 卡片更新失败（如 CardKit 间歇性 403）不应中断 agent turn：
		// 重建新进度卡片并替换 handle，重建失败则 handle 置空、下轮 flush 重试创建。
		p.recoverRichCardUpdateFailure(ctx, streamKey, rc, content, err)
		return nil
	}
	st.lock()
	st.lastProgressUpdate = time.Now()
	st.richCardFlushedPanelVersion = panelVer
	st.lastRichCardBodyStreamRunes = len([]rune(body))
	if !streaming && len(displaySteps) > 0 {
		st.finalizedSteps = append([]toolStep(nil), displaySteps...)
	}
	st.unlock()
	p.scheduleRichCardKeepalive(streamKey)
	return nil
}

// recoverRichCardUpdateFailure 在卡片更新失败时保持 turn 存活：直接重建新进度卡片
// 并替换 handle（旧卡片删除避免残留"运行中"）；重建失败则 handle 置空，下一轮
// flush 会再次走创建路径。错误不上抛，最终答复由普通消息兜底。
func (p *Platform) recoverRichCardUpdateFailure(ctx context.Context, streamKey agentkit.SessionID, rc replyContext, content string, cause error) {
	st := p.streamState(streamKey)
	st.lock()
	oldHandle := st.progressHandle
	st.progressHandle = nil
	st.unlock()

	slog.Warn(p.tag()+": rich card update failed, recreating progress card", "session_id", streamKey, "error", cause)
	newHandle, err := p.createProgressCard(ctx, streamKey, rc, content)
	if err != nil {
		if !errors.Is(err, errRichCardCreateCooldown) {
			slog.Warn(p.tag()+": recreate rich card failed, retry on next flush", "session_id", streamKey, "error", err)
		}
		return
	}
	st.lock()
	st.progressHandle = newHandle
	st.bodyStreamClosed = false // 新实体流式模式重新可用
	st.enqueueCard(streamCardProgress, newHandle)
	evicted := p.evictStreamCardsLocked(st)
	st.lastProgressUpdate = time.Now()
	st.richCardFlushedPanelVersion = st.richCardPanelVersion
	st.lastRichCardBodyStreamRunes = len([]rune(st.richCardDisplayBody()))
	st.unlock()
	p.deleteEvictedProgressCards(ctx, evicted)
	if oldHandle != nil && oldHandle != newHandle {
		if err := p.DeletePreviewMessage(ctx, oldHandle); err != nil {
			slog.Debug(p.tag()+": delete broken progress card failed", "session_id", streamKey, "error", err)
		}
	}
	p.scheduleRichCardKeepalive(streamKey)
}

// errRichCardCreateCooldown 表示卡片创建处于失败退避期，本次不真正发起 API 调用。
var errRichCardCreateCooldown = errors.New("rich card create in failure cooldown")

const (
	// richCardCreateRetryBase 是创建失败退避的起步间隔，按连续失败次数指数增长。
	richCardCreateRetryBase = 2 * time.Second
	// richCardCreateRetryMax 是退避上限，保证故障恢复后最多 30s 内重建卡片。
	richCardCreateRetryMax = 30 * time.Second
)

// createProgressCard 包装 SendPreviewStart，带创建失败退避：网关持续故障（如出口
// 代理 403）时，流式 delta 会频繁触发 flush，若每次都打创建 API 会进一步压垮链路。
// 连续失败按 2s/4s/8s…（上限 30s）退避，成功后清零。
func (p *Platform) createProgressCard(ctx context.Context, streamKey agentkit.SessionID, rc replyContext, content string) (any, error) {
	st := p.streamState(streamKey)
	st.lock()
	if time.Now().Before(st.nextCreateAfter) {
		st.unlock()
		return nil, errRichCardCreateCooldown
	}
	st.unlock()

	handle, err := p.SendPreviewStart(ctx, rc, content)
	st.lock()
	if err != nil {
		st.createFailCount++
		shift := st.createFailCount - 1
		if shift > 4 {
			shift = 4
		}
		backoff := richCardCreateRetryBase << shift
		if backoff > richCardCreateRetryMax {
			backoff = richCardCreateRetryMax
		}
		st.nextCreateAfter = time.Now().Add(backoff)
	} else {
		st.createFailCount = 0
		st.nextCreateAfter = time.Time{}
	}
	st.unlock()
	return handle, err
}

// scheduleRichCardKeepalive 重置心跳定时器：卡片创建后若长时间无更新（LLM 推理
// 期间没有任何出站流量），CardKit 实体可能被回收或遭网关间歇性拒绝；心跳通过
// 轻量 flush（页脚 elapsed 随时间变化）保持卡片活跃。每次成功 flush 后调用。
func (p *Platform) scheduleRichCardKeepalive(streamKey agentkit.SessionID) {
	st := p.streamState(streamKey)
	st.lock()
	if st.progressHandle == nil {
		st.unlock()
		return
	}
	stopStreamTimer(&st.keepaliveTimer)
	sid := streamKey
	st.keepaliveTimer = time.AfterFunc(richCardKeepaliveInterval, func() {
		p.onRichCardKeepalive(sid)
	})
	st.unlock()
}

func (p *Platform) onRichCardKeepalive(streamKey agentkit.SessionID) {
	// 不能用 p.streamState：turn 结束后 state 已删除，重建会泄漏。
	raw, ok := p.streams.Load(streamKey)
	if !ok {
		return
	}
	st := raw.(*streamState)
	st.lock()
	stopStreamTimer(&st.keepaliveTimer)
	active := st.progressHandle != nil
	idle := active && time.Since(st.lastProgressUpdate) >= richCardKeepaliveInterval
	st.unlock()
	if !active {
		return
	}
	if !idle {
		// 刚有过成功 flush（其已尝试重排心跳，但被当前 firing 的定时器挡住），补排下一次。
		p.scheduleRichCardKeepalive(streamKey)
		return
	}
	if err := p.flushRichCard(context.Background(), streamKey, true); err != nil {
		slog.Debug(p.tag()+": rich card keepalive flush failed", "session_id", streamKey, "error", err)
	}
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

// handleRichProactiveAssistant applies non-streaming assistant outbound (e.g. loop
// step-limit notice) onto the in-flight rich card before turn/end.
func (p *Platform) handleRichProactiveAssistant(ctx context.Context, event agentkit.OutboundEvent) error {
	streamKey := outboundStreamKey(event)
	var msg agentkit.ModelMessage
	if err := json.Unmarshal(event.Data, &msg); err != nil {
		return err
	}
	text := strings.TrimSpace(assistantText(msg))
	if text == "" {
		return p.outbound.Handle(ctx, event)
	}
	st := p.streamState(streamKey)
	st.lock()
	st.bodyText = text
	st.finalizedBodyText = text
	st.unlock()
	if err := p.flushRichCard(ctx, streamKey, false); err != nil {
		return err
	}
	return p.outbound.SendAssistantMedia(ctx, event, msg)
}

// finalizeRichTurnEndAsync patches the CardKit entity without blocking turn/end teardown.
// 失败降级链保证最终答复不丢：patch 旧卡 → 另发一张最终卡 → 纯文本（带重试）。
func (p *Platform) finalizeRichTurnEndAsync(
	ctx context.Context,
	sessionID agentkit.SessionID,
	handle any,
	content string,
	bodyText string,
	botReplyID string,
	endData capsession.TurnEndData,
) {
	parent := context.WithoutCancel(ctx)
	go func() {
		delivered := false
		if handle != nil {
			if err := p.patchRichCard(parent, handle, content); err != nil {
				slog.Warn(p.tag()+": finalize patch rich card on turn end failed, sending a new final card", "session_id", sessionID, "error", err)
			} else {
				delivered = true
			}
		}
		if !delivered {
			if rc, ok := p.replyContextForStreamKey(sessionID); ok {
				if newHandle, err := p.SendPreviewStart(parent, rc, content); err != nil {
					slog.Warn(p.tag()+": send final rich card on turn end failed, falling back to plain text", "session_id", sessionID, "error", err)
				} else {
					delivered = true
					// 旧卡停留在「运行中」且内容可能不全，新卡已成最终卡，删除旧卡避免误导。
					if handle != nil && handle != newHandle {
						if err := p.DeletePreviewMessage(parent, handle); err != nil {
							slog.Debug(p.tag()+": delete stale progress card on turn end failed", "session_id", sessionID, "error", err)
						}
					}
				}
			}
		}
		if !delivered && bodyText != "" {
			p.sendFinalPlainTextWithRetry(parent, sessionID, bodyText)
		}
		p.addBotReplyEndReaction(botReplyID, endData, "")
	}()
}

// finalPlainTextRetryDelays 是纯文本兜底的重试间隔：网关故障可持续分钟级，
// 只试一次会在故障窗口内真正丢失最终答复。
var finalPlainTextRetryDelays = []time.Duration{5 * time.Second, 20 * time.Second, 60 * time.Second}

func (p *Platform) sendFinalPlainTextWithRetry(ctx context.Context, streamKey agentkit.SessionID, bodyText string) {
	delivery := deliveryFromStreamKey(streamKey)
	for attempt := 0; ; attempt++ {
		if err := p.sendText(ctx, delivery, bodyText); err != nil {
			if attempt >= len(finalPlainTextRetryDelays) {
				slog.Error(p.tag()+": plain text fallback on turn end failed after retries, final answer lost",
					"session_id", streamKey, "attempts", attempt+1, "error", err)
				return
			}
			slog.Warn(p.tag()+": plain text fallback on turn end failed, will retry",
				"session_id", streamKey, "attempt", attempt+1, "error", err)
			timer := time.NewTimer(finalPlainTextRetryDelays[attempt])
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
			continue
		}
		return
	}
}

func (p *Platform) handleRichTurnEnd(ctx context.Context, sessionID agentkit.SessionID, endData capsession.TurnEndData) error {
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
		p.finalizeRichTurnEndAsync(ctx, sessionID, handle, content, bodyText, botReplyID, endData)
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
