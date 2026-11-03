package feishu

import (
	"context"
	"encoding/json"
	"errors"
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

const richCardBodySegmentSeparator = "\n\n"
const outboundStreamReplySuffix = ":reply:"

// outboundStreamAsyncSuffix 把后台委派 job 的进度卡挂到 streams 的独立 key 下，
// 与父 turn 回复卡共用同一套 flush / 心跳 / 重建逻辑。
const outboundStreamAsyncSuffix = ":async:"

func asyncStreamKey(streamKey agentkit.SessionID, jobID string) agentkit.SessionID {
	return agentkit.SessionID(string(streamKey) + outboundStreamAsyncSuffix + jobID)
}

// outboundStreamKey isolates in-flight card state per agent turn when TurnID is
// set (preferred); otherwise per trigger message (delivery + Route.ReplyTo).
func outboundStreamKey(event agentkit.OutboundEvent) agentkit.SessionID {
	if id := strings.TrimSpace(event.TurnID); id != "" {
		return agentkit.SessionID(id)
	}
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
	return p.flushRichCardState(ctx, streamKey, p.streamState(streamKey), streaming)
}

// Timers retain the originating state so a late callback cannot recreate or
// update a completed turn (or another turn reusing the same legacy key).
func (p *Platform) flushRichCardState(ctx context.Context, streamKey agentkit.SessionID, st *streamState, streaming bool) error {
	st.lock()
	if st.ended {
		st.unlock()
		return nil
	}
	route, hasRoute := st.route, st.hasRoute
	st.unlock()
	var rc replyContext
	var ok bool
	if hasRoute {
		rc, ok = p.replyContextFromRoute(route)
	} else {
		rc, ok = p.deliveryForSend(deliveryFromStreamKey(streamKey))
		if anchor := replyToFromStreamKey(streamKey); anchor != "" {
			rc.messageID = anchor
		}
	}
	if !ok {
		return nil
	}

	st.lock()
	if st.ended {
		st.unlock()
		return nil
	}
	status := st.status
	if status == "" {
		status = cardStatusThinking
	}
	// 更新模式：每次 flush 渲染全量快照（正文 + 全部步骤），同一张卡原地更新。
	var body string
	var fullBody string
	if st.bodyFn != nil {
		body = st.bodyFn(streaming)
		fullBody = body
	} else {
		fullBody = st.richCardDisplayBody()
		if strings.TrimSpace(fullBody) != "" {
			st.finalizedBodyText = fullBody
		}
		body = fullBody
	}
	allSteps := p.renderRichSteps(st)
	displaySteps := allSteps
	handle := st.progressHandle
	elapsed := progressElapsed(st)
	panelVer := st.richCardPanelVersion
	canStreamBody := streaming && p.canRichCardStreamBody(st, handle)
	st.unlock()

	content := buildRichCard(status, "", displaySteps, body, streaming, elapsed)
	if strings.TrimSpace(content) == "" || content == " " {
		return nil
	}

	if handle == nil {
		_, err := p.createProgressCard(ctx, st, rc, content)
		if err != nil {
			// 卡片创建失败不应中断 agent turn；最终答复仍走普通消息路径。
			if !errors.Is(err, errRichCardCreateCooldown) {
				slog.Warn(p.tag()+": rich card create failed, continue without progress card", "session_id", streamKey, "error", err)
			}
			return nil
		}
		st.lock()
		if st.ended {
			st.unlock()
			return nil
		}
		st.lastProgressUpdate = time.Now()
		st.richCardFlushedPanelVersion = panelVer
		st.lastRichCardBodyStreamRunes = len([]rune(body))
		st.unlock()
		p.scheduleRichCardKeepalive(streamKey, st)
		return nil
	}

	if canStreamBody {
		h := handle.(*feishuPreviewHandle)
		streamText := streamBodyMarkdownForCardKit(body)
		if strings.TrimSpace(streamText) != "" {
			// 中途帧用 quick 预算：失败快速返回，内容累积等下一帧补齐，
			// 避免故障期多重重试持续锤网关并长时间占用同卡写锁。
			streamErr := p.streamCardElementByID(ctx, h, richCardMainTextElementID, streamText, retryProfileQuick)
			if streamErr == nil {
				st.lock()
				st.lastProgressUpdate = time.Now()
				st.updateFailCount = 0
				st.lastRichCardBodyStreamRunes = len([]rune(body))
				st.unlock()
				p.scheduleRichCardKeepalive(streamKey, st)
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
		err = p.updateMessageProfile(ctx, handle, content, retryProfileQuick)
	} else {
		// message/end 段定稿：标准预算即可，turn/end 定稿负责最终投递保证。
		err = p.patchRichCard(ctx, handle, content, retryProfileStandard)
	}
	if err != nil {
		// 更新模式：进度卡只原地更新，中途失败绝不重建新
		// 消息。streamState 内容单调累积，跳过一帧什么都不丢——下一次成功的
		// flush 会把同一张卡补齐到最新。turn/end 会尝试定稿；
		// 未知写入结果不会触发另发消息，中途失败只记日志并等下轮。
		st.lock()
		st.updateFailCount++
		fails := st.updateFailCount
		st.unlock()
		slog.Warn(p.tag()+": rich card update failed, keep card and retry on next flush",
			"session_id", streamKey, "consecutive_failures", fails, "error", err)
		p.scheduleRichCardKeepalive(streamKey, st)
		return nil
	}
	st.lock()
	st.lastProgressUpdate = time.Now()
	st.updateFailCount = 0
	st.richCardFlushedPanelVersion = panelVer
	st.lastRichCardBodyStreamRunes = len([]rune(body))
	if !streaming && len(displaySteps) > 0 {
		st.finalizedSteps = append([]toolStep(nil), displaySteps...)
	}
	st.unlock()
	p.scheduleRichCardKeepalive(streamKey, st)
	return nil
}

// errRichCardCreateCooldown 表示卡片创建处于失败退避期，本次不真正发起 API 调用。
var errRichCardCreateCooldown = errors.New("rich card create in failure cooldown")

const (
	// richCardCreateRetryBase 是创建失败退避的起步间隔，按连续失败次数指数增长。
	richCardCreateRetryBase = 2 * time.Second
	// richCardCreateRetryMax 是退避上限，保证故障恢复后最多 30s 内重建卡片。
	richCardCreateRetryMax = 30 * time.Second
)

// createProgressCard serializes creation and publishes its handle before opening
// the gate. Confirmed failures back off 2s/4s/8s (up to 30s); unknown results
// latch for the originating state and are never retried by subsequent flushes.
func (p *Platform) createProgressCard(ctx context.Context, st *streamState, rc replyContext, content string) (any, error) {
	st.lock()
	if st.progressHandle != nil {
		handle := st.progressHandle
		st.unlock()
		return handle, nil
	}
	if st.ended {
		st.unlock()
		return nil, errRichCardCreateCooldown
	}
	if st.unknownCreateErr != nil {
		err := st.unknownCreateErr
		st.unlock()
		return nil, err
	}
	if st.createDone != nil || time.Now().Before(st.nextCreateAfter) {
		st.unlock()
		return nil, errRichCardCreateCooldown
	}
	st.createDone = make(chan struct{})
	st.unlock()

	handle, err := p.SendPreviewStart(ctx, rc, content)
	st.lock()

	if isMessageWriteUnknown(err) {
		st.unknownCreateErr = err
	}
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
		st.progressHandle = handle
		st.bodyStreamClosed = false
	}
	close(st.createDone)
	st.createDone = nil
	st.unlock()
	return handle, err
}

// scheduleRichCardKeepalive 重置心跳定时器：卡片创建后若长时间无更新（LLM 推理
// 期间没有任何出站流量），CardKit 实体可能被回收或遭网关间歇性拒绝；心跳通过
// 轻量 flush（页脚 elapsed 随时间变化）保持卡片活跃。每次成功 flush 后调用。
func (p *Platform) scheduleRichCardKeepalive(streamKey agentkit.SessionID, st *streamState) {
	st.lock()
	if st.ended || st.progressHandle == nil {
		st.unlock()
		return
	}
	stopStreamTimer(&st.keepaliveTimer)
	sid := streamKey
	st.keepaliveTimer = time.AfterFunc(richCardKeepaliveInterval, func() {
		p.onRichCardKeepalive(sid, st)
	})
	st.unlock()
}

func (p *Platform) onRichCardKeepalive(streamKey agentkit.SessionID, st *streamState) {
	st.lock()
	stopStreamTimer(&st.keepaliveTimer)
	active := !st.ended && st.progressHandle != nil
	idle := active && time.Since(st.lastProgressUpdate) >= richCardKeepaliveInterval
	st.unlock()
	if !active {
		return
	}
	if !idle {
		// 刚有过成功 flush（其已尝试重排心跳，但被当前 firing 的定时器挡住），补排下一次。
		p.scheduleRichCardKeepalive(streamKey, st)
		return
	}
	if err := p.flushRichCardState(context.Background(), streamKey, st, true); err != nil {
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
	p.scheduleBodyFlush(sessionID, st)
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
	raw, ok := p.streams.Load(sessionID)
	if !ok {
		return
	}
	st := raw.(*streamState)
	st.lock()
	stopStreamTimer(&st.bodyFlushTimer)
	st.unlock()
}

func (p *Platform) scheduleBodyFlush(sessionID agentkit.SessionID, st *streamState) {
	st.lock()
	if st.ended || st.bodyFlushTimer != nil {
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
		active := !st.ended && (st.progressHandle != nil || len(st.steps) > 0 || strings.TrimSpace(st.thinking) != "" || strings.TrimSpace(st.richCardDisplayBody()) != "")
		st.unlock()
		if !active {
			return
		}
		if err := p.flushRichCardState(context.Background(), sid, st, true); err != nil {
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
	// 限频：间隔与最小增量两个条件同时满足才立即 flush，
	// 否则由防抖定时器在间隔边界兜底——快模型高频 delta 下写请求速率同样
	// 被压到 1/interval，避免打满出口代理（TLB 403）与 Lark 流式更新限流。
	shouldFlushNow := st.progressHandle == nil ||
		(elapsed >= interval && bodyGrowth >= richCardBodyStreamMinRunes)
	st.unlock()
	if shouldFlushNow {
		p.cancelBodyFlushTimer(sessionID)
		return p.flushRichCard(ctx, sessionID, true)
	}
	p.scheduleBodyFlush(sessionID, st)
	return nil
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
	success := strings.TrimSpace(result.Audit["decision"]) != "deny" && strings.TrimSpace(result.Audit["status"]) != "failed"
	status := "completed"
	if !success {
		status = "failed"
	}
	st.steps = append(st.steps, toolStep{
		Kind:    toolStepKindToolResult,
		Name:    name,
		Summary: content,
		Result:  content,
		Status:  status,
		CallID:  string(result.ID),
		Success: &success,
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
	streamKey := p.streamKeyForOutbound(event)
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
	streamKey := p.streamKeyForOutbound(event)
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
	streamKey := p.streamKeyForOutbound(event)
	p.cancelBodyFlushTimer(streamKey)
	var payload agentkit.MessageEndPayload
	fallbackText := ""
	hasToolCalls := false
	if err := json.Unmarshal(event.Data, &payload); err == nil {
		fallbackText = strings.TrimSpace(assistantText(payload.Message))
		hasToolCalls = len(payload.Message.ToolCalls) > 0
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
	if hasToolCalls {
		// Segment ends with tools: commit body text but keep the card in streaming
		// mode so tool steps, heartbeat, and later assistant text still refresh.
		st.commitInflightBodyText()
		st.bodyText = ""
		st.unlock()
		return p.flushRichCard(ctx, streamKey, true)
	}
	st.finalizedBodyText = st.richCardDisplayBody()
	// 无正文、无步骤且尚未建卡的消息段（如 ACP 零输出的 follow-up turn）
	// 不触发 flush：建卡只会得到一张空卡片气泡。
	nothingToShow := strings.TrimSpace(st.finalizedBodyText) == "" &&
		len(p.renderRichSteps(st)) == 0 &&
		st.progressHandle == nil
	st.unlock()
	if nothingToShow {
		return nil
	}
	// 整卡落盘（关闭 streaming、去掉进行中页脚），避免仅流式 main_text 时页脚残留在旧 JSON。
	return p.flushRichCard(ctx, streamKey, false)
}

// handleRichProactiveAssistant applies non-streaming assistant outbound (e.g. loop
// step-limit notice) onto the in-flight rich card before turn/end.
func (p *Platform) handleRichProactiveAssistant(ctx context.Context, event agentkit.OutboundEvent) error {
	streamKey := p.streamKeyForOutbound(event)
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
// Confirmed failures may fall back from patching the old card to a new final
// card and then plain text. An unknown create stops without another message.
func (p *Platform) finalizeRichTurnEndAsync(
	ctx context.Context,
	streamKey agentkit.SessionID,
	st *streamState,
	handle any,
	content string,
	bodyText string,
	botReplyID string,
	endData capsession.TurnEndData,
	delivery agentkit.SessionID,
	rc replyContext,
	hasReply bool,
) {
	parent := context.WithoutCancel(ctx)
	go func() {
		parent, cancel := context.WithTimeout(parent, 2*time.Minute)
		defer cancel()
		st.lock()
		pending := st.createDone
		st.unlock()
		if pending != nil {
			select {
			case <-pending:
			case <-parent.Done():
				slog.Warn(p.tag()+": final delivery unresolved while card creation is pending", "session_id", streamKey, "error", parent.Err())
				return
			}
		}
		st.lock()
		if handle == nil {
			handle = st.progressHandle
		}
		if botReplyID == "" {
			botReplyID = botReplyMessageID(st)
		}
		unknownCreate := st.unknownCreateErr
		st.unlock()
		if unknownCreate != nil {
			slog.Warn(p.tag()+": final delivery unresolved after unknown card creation", "session_id", streamKey, "error", unknownCreate)
			return
		}
		delivered := false
		if handle != nil {
			// 定稿 patch 用 finalize 预算（3 次、~4s）：只覆盖短暂抖动；
			// 窗口更长时快速进入降级链，最终投递由纯文本长重试兜底。
			if err := p.patchRichCard(parent, handle, content, retryProfileFinalize); err != nil {
				// CardKit 路径失败时尝试消息接口整卡更新（不同路径可能仍可用），
				// 挽救成功则旧卡直接成为最终卡，无需另发新卡。
				if salvageErr := p.UpdateMessage(parent, handle, content); salvageErr == nil {
					slog.Info(p.tag()+": finalize patch failed but salvaged via message patch", "session_id", streamKey, "error", err)
					delivered = true
				} else {
					slog.Warn(p.tag()+": finalize patch rich card on turn end failed, sending a new final card", "session_id", streamKey, "error", err)
				}
			} else {
				delivered = true
			}
		}
		if !delivered && hasReply {
			if newHandle, err := p.SendPreviewStart(parent, rc, content); err != nil {
				if isMessageWriteUnknown(err) {
					slog.Warn(p.tag()+": final card delivery unresolved; not sending a duplicate", "session_id", streamKey, "error", err)
					return
				}
				slog.Warn(p.tag()+": send final rich card on turn end failed, falling back to plain text", "session_id", streamKey, "error", err)
			} else {
				delivered = true
				// 新最终卡已送达：撤回过时的旧卡。与中途更新不同（旧卡是当时
				// 唯一内容，删除会留「撤回了一条消息」灰条且可能丢失进度），
				// 定稿时新卡已是完整答复，旧卡只是过时残影——撤回换来会话里
				// 只有一份答案。必须先发后撤，保证任何时刻答复不丢。
				if handle != nil {
					if delErr := p.DeletePreviewMessage(parent, handle); delErr != nil {
						slog.Warn(p.tag()+": recall stale progress card failed, leaving it in place", "session_id", streamKey, "error", delErr)
					}
				}
				_ = newHandle
			}
		}
		if !delivered && bodyText != "" && delivery != "" {
			p.sendFinalPlainTextWithRetry(parent, delivery, streamKey, bodyText)
		}
		p.addBotReplyEndReaction(botReplyID, endData, "")
	}()
}

// finalPlainTextRetryDelays 是纯文本兜底的重试间隔：网关故障可持续分钟级，
// 只试一次会在故障窗口内真正丢失最终答复。
var finalPlainTextRetryDelays = []time.Duration{5 * time.Second, 20 * time.Second, 60 * time.Second}

func (p *Platform) sendFinalPlainTextWithRetry(ctx context.Context, delivery agentkit.SessionID, streamKey agentkit.SessionID, bodyText string) {
	for attempt := 0; ; attempt++ {
		if err := p.sendText(ctx, delivery, bodyText); err != nil {
			if isMessageWriteUnknown(err) || ctx.Err() != nil {
				slog.Warn(p.tag()+": final plain delivery unresolved; not retrying", "session_id", streamKey, "error", err)
				return
			}
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
	if st.startedAt.IsZero() && !hasHandle && st.createDone == nil && st.unknownCreateErr == nil {
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
	// 定稿前停心跳：避免定稿 patch 与在途心跳 flush 竞争同一 card_id 的写锁/序号。
	stopStreamTimer(&st.keepaliveTimer)
	handle := st.progressHandle
	botReplyID := botReplyMessageID(st)
	fullBody := strings.TrimSpace(st.richCardDisplayBody())
	if fullBody == "" {
		fullBody = strings.TrimSpace(st.finalizedBodyText)
	}
	if fullBody == "" && len(endData.Message) > 0 {
		var msg agentkit.ModelMessage
		if err := json.Unmarshal(endData.Message, &msg); err == nil {
			fullBody = strings.TrimSpace(assistantText(msg))
		}
	}
	if endData.Cancelled && fullBody == "" {
		fullBody = cancelledBodyText(endData.StopReason)
	}
	allSteps := p.renderRichSteps(st)
	if len(allSteps) == 0 && len(st.finalizedSteps) > 0 {
		allSteps = append([]toolStep(nil), st.finalizedSteps...)
	}
	// 更新模式：定稿与纯文本兜底均使用全量内容，保证最终答复完整。
	bodyText := strings.TrimSpace(fullBody)
	displaySteps := allSteps
	elapsed := progressElapsed(st)
	st.status = finalStatus
	st.ended = true
	st.bodyText = ""
	st.unlock()

	rc, hasReply := p.replyContextForStream(sessionID)
	delivery := p.deliveryForStream(sessionID)

	content := buildRichCard(finalStatus, "", displaySteps, bodyText, false, elapsed)
	p.clearStreamState(sessionID)
	if strings.TrimSpace(content) != "" && content != " " {
		p.finalizeRichTurnEndAsync(ctx, sessionID, st, handle, content, bodyText, botReplyID, endData, delivery, rc, hasReply)
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

func (p *Platform) bootstrapReplyCard(ctx context.Context, streamKey agentkit.SessionID) error {
	return p.flushRichCard(ctx, streamKey, true)
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
