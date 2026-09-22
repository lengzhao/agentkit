package derive

import (
	"github.com/lengzhao/agentkit"
)

// repairToolPairing applies the second pass of pi-ai transformMessages: close
// orphaned tool-call rounds with stand-in results, skip non-replayable
// assistants, and drop tool results that are not answering the current round.
// Agentkit also drops orphan tool messages when no round is open (e.g. after
// force-compaction head drops) — pi wire conversion can still 400 on that shape.
func repairToolPairing(messages []agentkit.ModelMessage) []agentkit.ModelMessage {
	if len(messages) == 0 {
		return messages
	}

	result := make([]agentkit.ModelMessage, 0, len(messages))
	var pendingCalls []agentkit.ToolCall
	answered := make(map[agentkit.ToolCallID]bool)

	insertSyntheticToolResults := func() {
		if len(pendingCalls) == 0 {
			return
		}
		var batch []agentkit.ToolResult
		for _, call := range pendingCalls {
			if answered[call.ID] {
				continue
			}
			batch = append(batch, InterruptedToolResult(call))
		}
		if len(batch) > 0 {
			result = append(result, agentkit.ModelMessage{Role: "tool", ToolResults: batch})
		}
		pendingCalls = nil
		clear(answered)
	}

	for _, msg := range messages {
		if len(msg.ToolResults) > 0 {
			if len(pendingCalls) == 0 {
				continue
			}
			var kept []agentkit.ToolResult
			for _, tr := range msg.ToolResults {
				if !pendingToolCallID(pendingCalls, tr.ID) {
					continue
				}
				answered[tr.ID] = true
				kept = append(kept, tr)
			}
			if len(kept) == 0 {
				continue
			}
			msg.ToolResults = kept
			result = append(result, msg)
			continue
		}

		if msg.Role == "assistant" || len(msg.ToolCalls) > 0 {
			insertSyntheticToolResults()
			if nonReplayableAssistant(msg) {
				continue
			}
			if len(msg.ToolCalls) > 0 {
				pendingCalls = append([]agentkit.ToolCall(nil), msg.ToolCalls...)
				clear(answered)
			}
			result = append(result, msg)
			continue
		}

		if msg.Role == "user" {
			insertSyntheticToolResults()
			result = append(result, msg)
			continue
		}

		result = append(result, msg)
	}

	insertSyntheticToolResults()
	return result
}

func nonReplayableAssistant(msg agentkit.ModelMessage) bool {
	switch msg.StopReason {
	case agentkit.AssistantStopReasonError, agentkit.AssistantStopReasonAborted:
		return true
	default:
		return false
	}
}

func pendingToolCallID(calls []agentkit.ToolCall, id agentkit.ToolCallID) bool {
	for _, call := range calls {
		if call.ID == id {
			return true
		}
	}
	return false
}
