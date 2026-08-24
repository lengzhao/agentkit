package llm

import (
	"github.com/lengzhao/agentkit"
)

type streamAccumulator struct {
	done          bool
	streamStarted bool
	textStarted   bool
	thinkStarted  bool
	acc           agentkit.ModelMessage
	toolBuf       map[int]*agentkit.ToolCall
	pending       []agentkit.LLMEvent
	usage         *agentkit.Usage
}

func newStreamAccumulator() *streamAccumulator {
	return &streamAccumulator{
		toolBuf: make(map[int]*agentkit.ToolCall),
		acc:     agentkit.ModelMessage{Role: "assistant"},
	}
}

func (a *streamAccumulator) pushPending(events ...agentkit.LLMEvent) {
	a.pending = append(a.pending, events...)
}

func (a *streamAccumulator) recvPending() (agentkit.LLMEvent, bool) {
	if len(a.pending) == 0 {
		return agentkit.LLMEvent{}, false
	}
	ev := a.pending[0]
	a.pending = a.pending[1:]
	return ev, true
}

func (a *streamAccumulator) ensureStarted() {
	if a.streamStarted {
		return
	}
	a.streamStarted = true
	start := cloneMessage(a.acc)
	a.pushPending(agentkit.LLMEvent{Type: agentkit.AssistantEventStart, Message: start})
}

func (a *streamAccumulator) appendTextDelta(delta string) {
	if delta == "" {
		return
	}
	a.ensureStarted()
	if !a.textStarted {
		a.textStarted = true
		start := cloneMessage(a.acc)
		a.pushPending(agentkit.LLMEvent{Type: agentkit.AssistantEventTextStart, ContentIndex: 0, Message: start})
	}
	a.acc.Content = appendText(a.acc.Content, delta)
	msg := cloneMessage(a.acc)
	a.pushPending(agentkit.LLMEvent{
		Type:         agentkit.AssistantEventTextDelta,
		ContentIndex: 0,
		Delta:        delta,
		Message:      msg,
	})
}

func (a *streamAccumulator) appendThinkingDelta(delta string) {
	if delta == "" {
		return
	}
	a.ensureStarted()
	if !a.thinkStarted {
		a.thinkStarted = true
		start := cloneMessage(a.acc)
		a.pushPending(agentkit.LLMEvent{Type: agentkit.AssistantEventThinkingStart, ContentIndex: 0, Message: start})
	}
	a.acc.Content = appendThinking(a.acc.Content, delta)
	msg := cloneMessage(a.acc)
	a.pushPending(agentkit.LLMEvent{
		Type:         agentkit.AssistantEventThinkingDelta,
		ContentIndex: 0,
		Delta:        delta,
		Message:      msg,
	})
}

func (a *streamAccumulator) appendToolCallDelta(idx int, id, name, args string) {
	if id == "" && name == "" && args == "" {
		return
	}
	call, ok := a.toolBuf[idx]
	if !ok {
		a.ensureStarted()
		call = &agentkit.ToolCall{ID: agentkit.ToolCallID(id), Name: name}
		a.toolBuf[idx] = call
		if id != "" || name != "" {
			cp := *call
			msg := cloneMessage(a.acc)
			a.pushPending(agentkit.LLMEvent{
				Type:         agentkit.AssistantEventToolCallStart,
				ContentIndex: idx,
				ToolCall:     &cp,
				Message:      msg,
			})
		}
	}
	if id != "" {
		call.ID = agentkit.ToolCallID(id)
	}
	if name != "" {
		call.Name = name
	}
	if args != "" {
		call.Input = append(call.Input, args...)
		cp := *call
		msg := cloneMessage(a.acc)
		a.pushPending(agentkit.LLMEvent{
			Type:         agentkit.AssistantEventToolCallDelta,
			ContentIndex: idx,
			Delta:        args,
			ToolCall:     &cp,
			Message:      msg,
		})
	}
}

func (a *streamAccumulator) finalize() {
	for idx, call := range a.toolBuf {
		cp := *call
		a.acc.ToolCalls = append(a.acc.ToolCalls, cp)
		msg := cloneMessage(a.acc)
		a.pushPending(agentkit.LLMEvent{
			Type:         agentkit.AssistantEventToolCallEnd,
			ContentIndex: idx,
			ToolCall:     &cp,
			Message:      msg,
		})
	}
	a.done = true
	msg := a.acc
	a.pushPending(agentkit.LLMEvent{Type: agentkit.LLMEventMessage, Message: &msg, Usage: a.usage})
}

// setUsage records the token accounting a provider reports, so the final message
// event can carry it to the agent's run budget.
func (a *streamAccumulator) setUsage(input, output, total int) {
	if input == 0 && output == 0 && total == 0 {
		return
	}
	if total == 0 {
		total = input + output
	}
	a.usage = &agentkit.Usage{
		InputTokens:  input,
		OutputTokens: output,
		TotalTokens:  total,
	}
}

func appendText(parts []agentkit.ContentPart, text string) []agentkit.ContentPart {
	if len(parts) == 0 || parts[len(parts)-1].Type != "text" {
		return append(parts, agentkit.ContentPart{Type: "text", Text: text})
	}
	parts[len(parts)-1].Text += text
	return parts
}

func appendThinking(parts []agentkit.ContentPart, text string) []agentkit.ContentPart {
	if len(parts) == 0 || parts[len(parts)-1].Type != "thinking" {
		return append(parts, agentkit.ContentPart{Type: "thinking", Text: text})
	}
	parts[len(parts)-1].Text += text
	return parts
}

func cloneMessage(msg agentkit.ModelMessage) *agentkit.ModelMessage {
	cp := msg
	cp.Content = append([]agentkit.ContentPart(nil), msg.Content...)
	cp.ToolCalls = append([]agentkit.ToolCall(nil), msg.ToolCalls...)
	return &cp
}
