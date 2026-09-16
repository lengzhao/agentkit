package session

import (
	"context"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/workspace"
	rtmedia "github.com/lengzhao/agentkit/runtime/media"
)

func sanitizeMessagesForLLM(ctx context.Context, ws workspace.Service, msgs []agentkit.ModelMessage) []agentkit.ModelMessage {
	if ws == nil || len(msgs) == 0 {
		return msgs
	}
	out := make([]agentkit.ModelMessage, len(msgs))
	for i, msg := range msgs {
		out[i] = sanitizeMessageForLLM(ctx, ws, msg)
	}
	return out
}

func sanitizeMessageForLLM(ctx context.Context, ws workspace.Service, msg agentkit.ModelMessage) agentkit.ModelMessage {
	if len(msg.Content) > 0 {
		parts := make([]agentkit.ContentPart, len(msg.Content))
		for i, part := range msg.Content {
			parts[i] = part
			if src := part.Source; src != "" {
				parts[i].Source = rtmedia.AgentLLMPath(ctx, ws, src)
			}
			if part.Type == "text" || part.Type == "" {
				if t := part.Text; t != "" {
					parts[i].Text = rtmedia.RewritePathsInText(ctx, ws, t)
				}
			}
		}
		msg.Content = parts
	}
	if len(msg.ToolResults) > 0 {
		results := make([]agentkit.ToolResult, len(msg.ToolResults))
		for i, r := range msg.ToolResults {
			results[i] = r
			if r.Content != "" {
				results[i].Content = rtmedia.RewritePathsInText(ctx, ws, r.Content)
			}
			if r.Audit != nil {
				if spill := r.Audit[AuditSpillPath]; spill != "" {
					audit := make(map[string]string, len(r.Audit))
					for k, v := range r.Audit {
						audit[k] = v
					}
					audit[AuditSpillPath] = rtmedia.AgentLLMPath(ctx, ws, spill)
					results[i].Audit = audit
				}
			}
		}
		msg.ToolResults = results
	}
	return msg
}
