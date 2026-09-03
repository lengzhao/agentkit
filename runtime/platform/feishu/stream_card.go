package feishu

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"time"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/platform/common"
)

const maxToolSummaryRunes = 180
const progressHeartbeatInterval = 5 * time.Second

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
		st.handle != nil &&
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
	if err := p.flushRichStream(context.Background(), sessionID, true); err != nil {
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
		st.toolCallIDIdx = make(map[string]int)
	}
	return st
}

func (p *Platform) handleRichStreamUpdate(ctx context.Context, sessionID agentkit.SessionID, ame agentkit.AssistantMessageEvent) error {
	st := p.richStreamState(sessionID)
	st.mu.Lock()
	changed := p.applyRichStreamEvent(st, ame)
	shouldFlush := changed && (st.handle == nil || time.Since(st.lastUpdate) >= streamUpdateInterval)
	st.mu.Unlock()
	if !shouldFlush {
		return nil
	}
	return p.flushRichStream(ctx, sessionID, true)
}

func (p *Platform) applyRichStreamEvent(st *streamState, ame agentkit.AssistantMessageEvent) bool {
	switch ame.Type {
	case agentkit.AssistantEventTextDelta:
		if ame.Delta == "" {
			return false
		}
		st.text += ame.Delta
		st.status = cardStatusWorking
		return true
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
		if callID != "" {
			st.toolCallIDIdx[callID] = idx
		}
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
				st.toolCallIDIdx[callID] = idx
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
	st := p.richStreamState(event.SessionID)
	st.mu.Lock()
	changed := p.applyToolResult(st, result)
	shouldFlush := changed && (st.handle == nil || time.Since(st.lastUpdate) >= streamUpdateInterval)
	st.mu.Unlock()
	if !shouldFlush {
		return nil
	}
	return p.flushRichStream(ctx, event.SessionID, true)
}

func (p *Platform) handleRichStreamMessageEnd(ctx context.Context, event agentkit.OutboundEvent) error {
	var payload agentkit.MessageEndPayload
	if err := json.Unmarshal(event.Data, &payload); err == nil {
		if text := assistantText(payload.Message); text != "" {
			st := p.richStreamState(event.SessionID)
			st.mu.Lock()
			st.text = text
			st.mu.Unlock()
		}
	}
	return p.flushRichStream(ctx, event.SessionID, true)
}

func (p *Platform) handleRichTurnEnd(ctx context.Context, sessionID agentkit.SessionID) error {
	st := p.streamState(sessionID)
	st.mu.Lock()
	if st.startedAt.IsZero() {
		st.mu.Unlock()
		return nil
	}
	st.status = cardStatusDone
	st.mu.Unlock()
	if err := p.flushRichStream(ctx, sessionID, false); err != nil {
		return err
	}
	p.clearStream(sessionID)
	return nil
}

func (p *Platform) flushRichStream(ctx context.Context, sessionID agentkit.SessionID, streaming bool) error {
	rc, ok := p.deliveryFor(sessionID)
	if !ok {
		return nil
	}

	st := p.streamState(sessionID)
	st.mu.Lock()
	content := p.renderRichStreamContent(st, streaming)
	handle := st.handle
	empty := strings.TrimSpace(content) == "" || content == " "
	hasProgress := len(st.steps) > 0 || strings.TrimSpace(st.thinking) != ""
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
		st.handle = newHandle
		st.lastUpdate = time.Now()
		st.mu.Unlock()
		return nil
	}
	if err := p.UpdateMessage(ctx, handle, content); err != nil {
		return err
	}
	st.mu.Lock()
	st.lastUpdate = time.Now()
	st.mu.Unlock()
	return nil
}

func (p *Platform) renderRichStreamContent(st *streamState, streaming bool) string {
	elapsed := time.Since(st.startedAt)
	if p.progressStyle == "compact" {
		return p.renderCompactProgressCard(st, streaming)
	}
	steps := p.renderRichSteps(st)
	markdown := strings.TrimSpace(st.text)
	if markdown == "" && !streaming && len(steps) == 0 {
		return " "
	}
	return buildRichCard(st.status, "", steps, markdown, streaming, elapsed)
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
	items := make([]common.ProgressCardEntry, 0, len(st.steps)+2)
	if p.showThinking && strings.TrimSpace(st.thinking) != "" {
		items = append(items, common.ProgressCardEntry{
			Kind: common.ProgressEntryThinking,
			Text: strings.TrimSpace(st.thinking),
		})
	}
	for _, step := range st.steps {
		if step.Kind != toolStepKindTool {
			continue
		}
		items = append(items, common.ProgressCardEntry{
			Kind: common.ProgressEntryToolUse,
			Tool: step.Name,
			Text: step.Summary,
		})
	}
	for _, step := range st.steps {
		if step.Kind != toolStepKindToolResult {
			continue
		}
		success := true
		if step.Success != nil {
			success = *step.Success
		}
		items = append(items, common.ProgressCardEntry{
			Kind:    common.ProgressEntryToolResult,
			Tool:    step.Name,
			Text:    step.Result,
			Status:  step.Status,
			Success: &success,
		})
	}
	if text := strings.TrimSpace(st.text); text != "" {
		items = append(items, common.ProgressCardEntry{
			Kind: common.ProgressEntryInfo,
			Text: text,
		})
	}
	if len(items) == 0 {
		return buildCardJSON(" ")
	}
	payload := &common.ProgressCardPayload{
		Version: 1,
		State:   state,
		Items:   items,
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
