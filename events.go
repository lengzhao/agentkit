package agentkit

const (
	EventTurnStart       EventType = "turn/start"
	EventTurnEnd         EventType = "turn/end"
	EventStepStart       EventType = "step/start"
	EventStepEnd         EventType = "step/end"
	EventUserMessage     EventType = "user/message"
	EventAssistantChunk  EventType = "assistant/chunk"
	EventAssistantMessage EventType = "assistant/message"
	EventToolCall        EventType = "tool/call"
	EventToolResult      EventType = "tool/result"
)
