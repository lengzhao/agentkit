package agentkit

const (
	EventTurnStart       EventType = "turn/start"
	EventTurnEnd         EventType = "turn/end"
	EventStepStart       EventType = "step/start"
	EventStepEnd         EventType = "step/end"
	EventUserMessage      EventType = "user/message"
	EventMessageStart     EventType = "message/start"
	EventMessageUpdate    EventType = "message/update"
	EventMessageEnd       EventType = "message/end"
	EventAssistantChunk   EventType = "assistant/chunk"
	EventAssistantMessage EventType = "assistant/message"
	EventToolCall        EventType = "tool/call"
	EventToolResult      EventType = "tool/result"
	EventCompaction      EventType = "session/compaction"
	EventSkillLoad       EventType = "skill/load"
)
