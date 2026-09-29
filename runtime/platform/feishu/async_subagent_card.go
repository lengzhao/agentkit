package feishu

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/session/sessevents"
)

// 后台委派（async subagent）进度卡 = p.streams 里一个独立 key 的普通 stream。
// 本文件只负责「哪些事件路由到哪个 stream」；卡片的创建 / 刷新 / 心跳 / 失败重建
// 全部复用 flushRichCard 同一条路径，不存在第二套卡片生命周期逻辑。
//
// 每个 job 的 stream state 挂在 asyncStreamKey(父 streamKey, jobID) 下，正文由
// streamState.bodyFn 自定义（renderAsyncSubagentBody），折叠区复用
// applyRichStreamEvent / applyToolResult / renderRichSteps。

// asyncSubagentCard 只保存事件路由所需的元数据。
type asyncSubagentCard struct {
	jobID         string
	streamKey     agentkit.SessionID // 父 turn 的 stream key（事件按它路由进来）
	asyncKey      agentkit.SessionID // 该 job 在 p.streams 里的独立 key
	parentAgentID agentkit.AgentID
}

func (p *Platform) asyncSubagentCardEnabled() bool {
	return p.asyncSubagentProgressCard && p.showToolProgress && p.useInteractiveCard
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

func (p *Platform) handleAsyncSubagentStart(ctx context.Context, streamKey agentkit.SessionID, parentAgent agentkit.AgentID, data sessevents.SubagentStartData) error {
	jobID := strings.TrimSpace(data.JobID)
	if jobID == "" {
		jobID = strings.TrimSpace(data.Session)
	}
	if jobID == "" {
		return nil
	}
	if existing := p.asyncSubagentForStream(streamKey); existing != nil {
		p.clearStream(existing.asyncKey)
		p.unregisterAsyncSubagentCard(existing)
	}

	agent := subagentDisplayName(data.Agent)
	task := truncateRunes(strings.TrimSpace(data.Task), maxToolSummaryRunes)
	asyncKey := asyncStreamKey(streamKey, jobID)

	st := p.streamState(asyncKey)
	st.lock()
	st.toolStepIdx = make(map[int]int)
	st.status = cardStatusWorking
	st.startedAt = time.Now()
	st.progressStartedAt = st.startedAt
	st.bodyFn = func(streaming bool) string {
		return renderAsyncSubagentBody(agent, task, st.status, streaming)
	}
	appendSubagentStartStep(p, st, agent, data.Task)
	st.unlock()

	p.registerAsyncSubagentCard(&asyncSubagentCard{
		jobID:         jobID,
		streamKey:     streamKey,
		asyncKey:      asyncKey,
		parentAgentID: parentAgent,
	})
	return p.flushRichCard(ctx, asyncKey, true)
}

func (p *Platform) handleAsyncSubagentEnd(ctx context.Context, data sessevents.SubagentEndData) error {
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

	st := p.streamState(card.asyncKey)
	st.lock()
	appendSubagentEndStep(p, st, data)
	status := strings.TrimSpace(data.Status)
	switch {
	case status == "failed" || status == "error" || data.Error != "":
		st.status = cardStatusError
	case status == "running":
		st.status = cardStatusWorking
	default:
		st.status = cardStatusDone
	}
	st.unlock()

	// 定稿整卡（失败已在 flushRichCard 内部降级，不会上抛），随后清理 state 与心跳。
	err := p.flushRichCard(ctx, card.asyncKey, false)
	p.clearStream(card.asyncKey)
	p.unregisterAsyncSubagentCard(card)
	return err
}

// renderAsyncSubagentBody 渲染后台委派进度卡的 main_text 正文。
// 调用方须持有对应 stream state 的锁（经 streamState.bodyFn 调用时已满足）。
func renderAsyncSubagentBody(agent, task string, status cardStatus, streaming bool) string {
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

func (p *Platform) handleAsyncSubagentStreamUpdate(ctx context.Context, streamKey agentkit.SessionID, event agentkit.OutboundEvent, ame agentkit.AssistantMessageEvent) (bool, error) {
	card := p.asyncSubagentForEvent(streamKey, event)
	if card == nil {
		return false, nil
	}
	seg := streamSegmentOfAssistantEvent(p, ame)
	if seg == streamSegmentNone || seg == streamSegmentBody {
		return true, nil
	}
	st := p.streamState(card.asyncKey)
	st.lock()
	changed := p.applyRichStreamEvent(st, ame)
	st.unlock()
	return true, p.maybeFlushRichCard(ctx, card.asyncKey, changed)
}

func (p *Platform) handleAsyncSubagentToolResult(ctx context.Context, streamKey agentkit.SessionID, event agentkit.OutboundEvent, result agentkit.ToolResult) (bool, error) {
	card := p.asyncSubagentForEvent(streamKey, event)
	if card == nil {
		return false, nil
	}
	st := p.streamState(card.asyncKey)
	st.lock()
	changed := p.applyToolResult(st, result)
	st.unlock()
	return true, p.maybeFlushRichCard(ctx, card.asyncKey, changed)
}
