package feishu

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/platform/common"
)

const maxToolSummaryRunes = 180

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
		if name == "" && ame.ToolCall != nil {
			name = strings.TrimSpace(ame.ToolCall.Name)
		}
		if name == "" {
			name = "Tool"
		}
		st.steps = append(st.steps, toolStep{
			Kind:    toolStepKindTool,
			Name:    name,
			Summary: name,
			Status:  "running",
		})
		st.toolStepIdx[ame.ContentIndex] = len(st.steps) - 1
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
		if ame.ToolCall != nil {
			input := strings.TrimSpace(string(ame.ToolCall.Input))
			if input != "" {
				step.Summary = truncateRunes(input, maxToolSummaryRunes)
			}
			if name := strings.TrimSpace(ame.ToolCall.Name); name != "" {
				step.Name = name
			}
		}
		step.Status = "completed"
		step.Done = true
		okVal := true
		step.Success = &okVal
		st.steps[idx] = step
		return true
	default:
		return false
	}
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
		if step.Done {
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
