package rpcplatform

import (
	"encoding/json"
	"strings"

	"github.com/lengzhao/agentkit"
	capsession "github.com/lengzhao/agentkit/cap/session"
)

const maxContentIndex = 4096

// assembler turns one AgentKit turn into one Pi turn.
// toolcall_* deltas stay internal. tool_execution_* and a single agent_end go out.
type assembler struct {
	model string

	phase        string // idle | accepted | streaming
	suppress     bool
	settled      bool
	abort        bool
	agentStarted bool
	nextTS       int
	curTS        int
	text         strings.Builder
	thinking     strings.Builder
	finalized    map[int]bool
	startedTools map[string]bool
	last         *wireAssistant
	usage        *wireUsage
	commandID    string
}

func newAssembler(model string) *assembler {
	return &assembler{
		model:        model,
		phase:        "idle",
		finalized:    map[int]bool{},
		startedTools: map[string]bool{},
	}
}

func (a *assembler) setModel(model string) {
	a.model = model
}

func (a *assembler) accept(commandID string) {
	a.phase = "accepted"
	a.commandID = commandID
	a.abort = false
	a.settled = false
	a.agentStarted = false
	a.curTS = 0
	a.text.Reset()
	a.thinking.Reset()
	a.last = nil
	a.usage = nil
	a.finalized = map[int]bool{}
	a.startedTools = map[string]bool{}
}

func (a *assembler) busy() bool {
	return a.phase != "idle" || a.suppress
}

func (a *assembler) consume(event agentkit.OutboundEvent) []any {
	if a.suppress {
		if event.Type == agentkit.EventTurnEnd {
			a.suppress = false
		}
		return nil
	}
	switch event.Type {
	case agentkit.EventTurnStart:
		if a.phase == "idle" {
			a.accept("centaur.prompt")
		}
		a.agentStarted = true
		a.phase = "streaming"
		return []any{wireAgentStart{Type: "agent_start"}}
	case agentkit.EventMessageStart:
		a.bump()
		return nil
	case agentkit.EventMessageUpdate:
		return a.onUpdate(event.Data)
	case agentkit.EventMessageEnd:
		return a.onMessageEnd(event.Data)
	case agentkit.EventToolResult:
		return a.onToolResult(event.Data)
	case agentkit.EventTurnEnd:
		return a.onTurnEnd(event.Data)
	case "error":
		return a.onError()
	default:
		return nil
	}
}

// finishAborted closes a turn that has not emitted agent_start yet.
// Later outbound events for that runner turn are dropped through turn/end.
func (a *assembler) finishAborted() []any {
	if a.phase == "idle" && !a.agentStarted {
		return nil
	}
	if a.agentStarted {
		a.abort = true
		return nil
	}
	msg := a.assistantFromBuffers()
	msg.StopReason = "aborted"
	a.phase = "idle"
	a.suppress = true
	a.agentStarted = false
	return []any{
		wireAgentStart{Type: "agent_start"},
		wireAgentEnd{Type: "agent_end", Messages: []wireAssistant{msg}},
	}
}

func (a *assembler) bump() {
	a.nextTS++
	a.curTS = a.nextTS
	a.text.Reset()
	a.thinking.Reset()
}

func (a *assembler) ensureTS() {
	if a.curTS == 0 {
		a.bump()
	}
}

func (a *assembler) onUpdate(raw json.RawMessage) []any {
	var payload agentkit.MessageUpdatePayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil
	}
	if payload.Usage != nil {
		a.usage = usageFrom(payload.Usage)
	}
	ame := payload.AssistantMessageEvent
	if ame.ContentIndex > maxContentIndex {
		return nil
	}
	switch ame.Type {
	case agentkit.AssistantEventTextDelta:
		if ame.Delta == "" {
			return nil
		}
		a.ensureTS()
		a.text.WriteString(ame.Delta)
		return []any{a.update("text_delta", ame.ContentIndex, ame.Delta)}
	case agentkit.AssistantEventThinkingDelta:
		if ame.Delta == "" {
			return nil
		}
		a.ensureTS()
		a.thinking.WriteString(ame.Delta)
		return []any{a.update("thinking_delta", ame.ContentIndex, ame.Delta)}
	case agentkit.AssistantEventToolCallEnd:
		return a.toolStart(ame)
	default:
		return nil
	}
}

func (a *assembler) update(typ string, index int, delta string) wireUpdate {
	return wireUpdate{
		Type: "message_update",
		Message: wireID{
			Role:      "assistant",
			Timestamp: a.curTS,
		},
		AssistantMessageEvent: wireAME{
			Type:         typ,
			ContentIndex: index,
			Delta:        delta,
		},
	}
}

func (a *assembler) toolStart(ame agentkit.AssistantMessageEvent) []any {
	call := ame.ToolCall
	id := ame.ID
	name := ame.ToolName
	var input json.RawMessage
	if call != nil {
		if id == "" {
			id = string(call.ID)
		}
		if name == "" {
			name = call.Name
		}
		input = call.Input
	}
	if id == "" {
		return nil
	}
	if a.startedTools[id] {
		return nil
	}
	a.startedTools[id] = true
	return []any{wireToolStart{
		Type:       "tool_execution_start",
		ToolCallID: id,
		ToolName:   name,
		Args:       objectArgs(input),
	}}
}

func (a *assembler) onToolResult(raw json.RawMessage) []any {
	var result agentkit.ToolResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil
	}
	id := string(result.ID)
	if id == "" {
		return nil
	}
	var out []any
	if !a.startedTools[id] {
		a.startedTools[id] = true
		name := result.Name
		out = append(out, wireToolStart{
			Type:       "tool_execution_start",
			ToolCallID: id,
			ToolName:   name,
			Args:       map[string]any{},
		})
	}
	out = append(out, wireToolEnd{
		Type:       "tool_execution_end",
		ToolCallID: id,
		IsError:    toolIsError(result),
		Result: wireResult{Content: []wireText{{
			Type: "text",
			Text: result.Content,
		}}},
	})
	return out
}

func (a *assembler) onMessageEnd(raw json.RawMessage) []any {
	var payload agentkit.MessageEndPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil
	}
	a.ensureTS()
	if a.finalized[a.curTS] {
		return nil
	}
	msg := a.assistantFromMessage(payload.Message, piStopMessage(payload.Message.StopReason))
	a.finalized[a.curTS] = true
	a.last = &msg
	return []any{wireMessageEnd{Type: "message_end", Message: msg}}
}

func (a *assembler) onTurnEnd(raw json.RawMessage) []any {
	if a.settled {
		a.settled = false
		a.phase = "idle"
		a.agentStarted = false
		a.abort = false
		return nil
	}
	var end capsession.TurnEndData
	_ = json.Unmarshal(raw, &end)
	msg := a.finalAssistant(end)
	msg.StopReason = piStopTurn(msg.StopReason, end, a.abort)
	a.phase = "idle"
	a.agentStarted = false
	a.abort = false
	a.suppress = false
	a.settled = true
	return []any{wireAgentEnd{Type: "agent_end", Messages: []wireAssistant{msg}}}
}

func (a *assembler) onError() []any {
	if a.settled || (a.phase == "idle" && !a.agentStarted) {
		return nil
	}
	a.settled = true
	if a.agentStarted {
		msg := a.assistantFromBuffers()
		msg.StopReason = "error"
		a.phase = "idle"
		a.agentStarted = false
		return []any{wireAgentEnd{Type: "agent_end", Messages: []wireAssistant{msg}}}
	}
	if a.phase == "idle" {
		return nil
	}
	id := a.commandID
	if id != "centaur.steer" {
		id = "centaur.prompt"
	}
	a.phase = "idle"
	a.suppress = false
	return []any{wireCentaurError{Type: "centaur_error", ID: id, Code: "pi_prompt_failed"}}
}

func (a *assembler) finalAssistant(end capsession.TurnEndData) wireAssistant {
	if len(end.Message) > 0 {
		var msg agentkit.ModelMessage
		if err := json.Unmarshal(end.Message, &msg); err == nil && (msg.Role != "" || len(msg.Content) > 0 || len(msg.ToolCalls) > 0) {
			a.ensureTS()
			return a.assistantFromMessage(msg, piStopMessage(msg.StopReason))
		}
	}
	if a.last != nil {
		msg := *a.last
		return msg
	}
	return a.assistantFromBuffers()
}

func (a *assembler) assistantFromBuffers() wireAssistant {
	a.ensureTS()
	var content []any
	if a.text.Len() > 0 {
		content = append(content, textPart{Type: "text", Text: a.text.String()})
	}
	if a.thinking.Len() > 0 {
		content = append(content, thinkingPart{Type: "thinking", Thinking: a.thinking.String()})
	}
	if content == nil {
		content = []any{}
	}
	return wireAssistant{
		Role:       "assistant",
		Timestamp:  a.curTS,
		Model:      a.model,
		StopReason: "stop",
		Content:    content,
		Usage:      a.usage,
	}
}

func (a *assembler) assistantFromMessage(msg agentkit.ModelMessage, stop string) wireAssistant {
	a.ensureTS()
	var content []any
	for _, part := range msg.Content {
		switch part.Type {
		case "", "text":
			if part.Text == "" && part.Type == "" {
				continue
			}
			content = append(content, textPart{Type: "text", Text: part.Text})
		case "thinking":
			content = append(content, thinkingPart{Type: "thinking", Thinking: part.Text})
		}
	}
	if a.thinking.Len() > 0 && !hasThinking(content) {
		content = append(content, thinkingPart{Type: "thinking", Thinking: a.thinking.String()})
	}
	if len(content) == 0 && a.text.Len() > 0 {
		content = append(content, textPart{Type: "text", Text: a.text.String()})
	}
	for _, call := range msg.ToolCalls {
		content = append(content, toolCallPart{
			Type:      "toolCall",
			ID:        string(call.ID),
			Name:      call.Name,
			Arguments: argumentsValue(call.Input),
		})
	}
	if content == nil {
		content = []any{}
	}
	return wireAssistant{
		Role:       "assistant",
		Timestamp:  a.curTS,
		Model:      a.model,
		StopReason: stop,
		Content:    content,
		Usage:      a.usage,
	}
}

func hasThinking(content []any) bool {
	for _, part := range content {
		if _, ok := part.(thinkingPart); ok {
			return true
		}
	}
	return false
}

func piStopMessage(reason string) string {
	switch reason {
	case "stop", "length", "toolUse", "aborted", "error":
		return reason
	default:
		return "stop"
	}
}

func piStopTurn(current string, end capsession.TurnEndData, abort bool) string {
	if abort || end.Cancelled || current == "aborted" || end.StopReason == "aborted" {
		return "aborted"
	}
	if end.Failed || current == "error" {
		return "error"
	}
	if current == "length" || end.StopReason == agentkit.AssistantStopReasonLength || end.StopReason == "length" {
		return "length"
	}
	return "stop"
}

func usageFrom(u *agentkit.Usage) *wireUsage {
	if u == nil {
		return nil
	}
	return &wireUsage{
		Input:       u.InputTokens,
		Output:      u.OutputTokens,
		TotalTokens: u.TotalTokens,
	}
}

func objectArgs(raw json.RawMessage) map[string]any {
	if len(raw) == 0 {
		return map[string]any{}
	}
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil || obj == nil {
		return map[string]any{}
	}
	return obj
}

func argumentsValue(raw json.RawMessage) any {
	if len(raw) == 0 {
		return map[string]any{}
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return string(raw)
	}
	if v == nil {
		return map[string]any{}
	}
	return v
}

func toolIsError(result agentkit.ToolResult) bool {
	if result.Audit == nil {
		return false
	}
	switch strings.TrimSpace(result.Audit["decision"]) {
	case "deny", "timeout", "error":
		return true
	}
	return strings.TrimSpace(result.Audit["status"]) == "failed"
}
