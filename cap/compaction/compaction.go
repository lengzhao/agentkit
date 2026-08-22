package compaction

import (
	"context"

	"github.com/lengzhao/agentkit"
)

type Service interface {
	Compact(context.Context, Request) (Result, error)
}

type Request struct {
	SessionID agentkit.SessionID
	AgentID   agentkit.AgentID
	Session   agentkit.Session
	Messages  []agentkit.ModelMessage
	// Force skips automatic thresholds such as minMessages (manual /compact).
	Force bool
}

type Result struct {
	Applied  bool
	Event    agentkit.SessionEvent
	Messages []agentkit.ModelMessage
}

type EventData struct {
	BeforeSeq agentkit.EventSeq  `json:"beforeSeq"`
	Summary   agentkit.ModelMessage `json:"summary"`
	Kind      string             `json:"kind"`
}

const (
	KindSummary = "summary"
	KindPrune   = "prune"
)

// PruneToolResults truncates oversized tool result text deterministically.
func PruneToolResults(messages []agentkit.ModelMessage, maxBytes int) []agentkit.ModelMessage {
	if maxBytes <= 0 {
		return messages
	}
	out := make([]agentkit.ModelMessage, len(messages))
	for i, msg := range messages {
		out[i] = msg
		if msg.Role != "tool" && len(msg.ToolResults) == 0 {
			continue
		}
		out[i] = pruneMessage(msg, maxBytes)
	}
	return out
}

func pruneMessage(msg agentkit.ModelMessage, maxBytes int) agentkit.ModelMessage {
	out := msg
	out.Content = pruneParts(msg.Content, maxBytes)
	if len(msg.ToolResults) > 0 {
		results := make([]agentkit.ToolResult, len(msg.ToolResults))
		for i, result := range msg.ToolResults {
			results[i] = result
			results[i].Content = pruneToolContent(result.Content, maxBytes)
		}
		out.ToolResults = results
	}
	return out
}

func pruneParts(parts []agentkit.ContentPart, maxBytes int) []agentkit.ContentPart {
	if len(parts) == 0 {
		return parts
	}
	out := make([]agentkit.ContentPart, len(parts))
	for i, part := range parts {
		out[i] = part
		if part.Type == "text" && len(part.Text) > maxBytes {
			out[i].Text = part.Text[:maxBytes] + "\n...[truncated]"
		}
	}
	return out
}

func pruneToolContent(parts []agentkit.ContentPart, maxBytes int) []agentkit.ContentPart {
	if len(parts) == 0 {
		return parts
	}
	out := make([]agentkit.ContentPart, len(parts))
	for i, part := range parts {
		out[i] = part
		if part.Type == "text" && len(part.Text) > maxBytes {
			out[i].Text = part.Text[:maxBytes] + "\n...[truncated]"
		}
	}
	return out
}

// TruncateToolResult truncates oversized tool result content after execution.
func TruncateToolResult(result agentkit.ToolResult, maxBytes int) agentkit.ToolResult {
	if maxBytes <= 0 {
		return result
	}
	out := result
	out.Content = pruneToolContent(result.Content, maxBytes)
	return out
}
