package session

import "github.com/lengzhao/agentkit"

// ContextData records the execution directory associated with the following
// conversation history. Empty Cwd returns to the configured consumer defaults.
type ContextData struct {
	Cwd   string `json:"cwd"`
	Epoch string `json:"epoch"`
}

// ContextEvents returns the current model context while preserving the original
// event log for auditing. Compaction and normal message derivation share it.
func ContextEvents(events []agentkit.SessionEvent) []agentkit.SessionEvent {
	for i := len(events) - 1; i >= 0; i-- {
		if events[i].Type == agentkit.EventSessionContext {
			return events[i+1:]
		}
	}
	return events
}
