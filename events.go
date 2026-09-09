package agentkit

const (
	EventTurnStart               EventType = "turn/start"
	EventTurnEnd                 EventType = "turn/end"
	EventStepStart               EventType = "step/start"
	EventStepEnd                 EventType = "step/end"
	EventUserMessage             EventType = "user/message"
	EventMessageStart            EventType = "message/start"
	EventMessageUpdate           EventType = "message/update"
	EventMessageEnd              EventType = "message/end"
	EventAssistantMessage        EventType = "assistant/message"
	EventToolCall                EventType = "tool/call"
	EventToolResult              EventType = "tool/result"
	EventCompaction              EventType = "session/compaction"
	EventSkillLoad               EventType = "skill/load"
	EventAutoRetryStart          EventType = "retry/start"
	EventAutoRetryEnd            EventType = "retry/end"
	EventSummarizationRetryStart EventType = "summarization/retry/start"
	EventSummarizationRetryEnd   EventType = "summarization/retry/end"
	EventOverflowRecovery        EventType = "overflow/recovery"
	EventTurnContinue            EventType = "turn/continue"
	EventUsage                   EventType = "usage"
	EventTodoUpdate              EventType = "todo/update"
	EventRunFinish               EventType = "run/finish"
	EventSessionRecovery         EventType = "session/recovery"
	EventSubagentStart           EventType = "subagent/start"
	EventSubagentEnd             EventType = "subagent/end"
	EventPermissionRequest       EventType = "permission/request"
	EventPermissionResolved      EventType = "permission/resolved"
)

// AssistantMessageEventType mirrors Pi RPC assistantMessageEvent.type values.
type AssistantMessageEventType string

const (
	AssistantEventStart         AssistantMessageEventType = "start"
	AssistantEventTextStart     AssistantMessageEventType = "text_start"
	AssistantEventTextDelta     AssistantMessageEventType = "text_delta"
	AssistantEventTextEnd       AssistantMessageEventType = "text_end"
	AssistantEventThinkingStart AssistantMessageEventType = "thinking_start"
	AssistantEventThinkingDelta AssistantMessageEventType = "thinking_delta"
	AssistantEventThinkingEnd   AssistantMessageEventType = "thinking_end"
	AssistantEventToolCallStart AssistantMessageEventType = "toolcall_start"
	AssistantEventToolCallDelta AssistantMessageEventType = "toolcall_delta"
	AssistantEventToolCallEnd   AssistantMessageEventType = "toolcall_end"
	AssistantEventDone          AssistantMessageEventType = "done"
	AssistantEventError         AssistantMessageEventType = "error"
)

// AssistantMessageEvent is the wire-safe Pi RPC delta payload (no cumulative partial).
type AssistantMessageEvent struct {
	Type         AssistantMessageEventType `json:"type"`
	ContentIndex int                       `json:"contentIndex,omitempty"`
	Delta        string                    `json:"delta,omitempty"`
	Content      string                    `json:"content,omitempty"`
	ID           string                    `json:"id,omitempty"`
	ToolName     string                    `json:"toolName,omitempty"`
	ToolCall     *ToolCall                 `json:"toolCall,omitempty"`
	Reason       string                    `json:"reason,omitempty"`
	ErrorMessage string                    `json:"errorMessage,omitempty"`
}

// MessageStartPayload is emitted when assistant streaming begins.
type MessageStartPayload struct {
	Message ModelMessage `json:"message"`
}

// MessageUpdatePayload matches Pi RPC message_update events.
type MessageUpdatePayload struct {
	Usage                 *Usage                `json:"usage,omitempty"`
	AssistantMessageEvent AssistantMessageEvent `json:"assistantMessageEvent"`
}

// MessageEndPayload is emitted when an assistant message is finalized.
type MessageEndPayload struct {
	Message ModelMessage `json:"message"`
}
