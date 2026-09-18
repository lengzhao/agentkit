package derive

import (
	"github.com/lengzhao/agentkit"
)

// InterruptedToolResultText is the tool result synthesized for a call the
// process never got to answer. It is model-visible on purpose: the agent needs
// to know the call was cut off rather than silently succeeded.
const InterruptedToolResultText = "tool execution was interrupted before it produced a result (process exited); re-run it if the result is still needed"

// interruptedDecision marks synthesized results in the tool audit trail.
const interruptedDecision = "interrupted"

// InterruptedToolResult is the stand-in result for a call that never ran to
// completion.
func InterruptedToolResult(call agentkit.ToolCall) agentkit.ToolResult {
	return agentkit.ToolResult{
		ID:      call.ID,
		Name:    call.Name,
		Content: InterruptedToolResultText,
		Audit:   map[string]string{"decision": interruptedDecision},
	}
}

// ToolResultMessage wraps a tool result as a tool-role model message.
func ToolResultMessage(result agentkit.ToolResult) agentkit.ModelMessage {
	return agentkit.ModelMessage{
		Role: "tool",
		ToolResults: []agentkit.ToolResult{{
			ID:      result.ID,
			Name:    result.Name,
			Content: result.Content,
			Audit:   result.Audit,
		}},
	}
}
