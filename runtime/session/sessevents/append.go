package sessevents

import (
	"context"
	"encoding/json"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/rctx"
	"github.com/lengzhao/agentkit/runtime/session/derive"
)

// AppendMessage sanitizes and appends a user/assistant message event.
func AppendMessage(ctx context.Context, s agentkit.Session, agentID agentkit.AgentID, typ agentkit.EventType, msg agentkit.ModelMessage) error {
	logicalChars := 0
	switch typ {
	case agentkit.EventUserMessage, agentkit.EventAssistantMessage:
		logicalChars = derive.EstimateLogicalChars(msg)
		msg = derive.SanitizeModelMessageForStorageWS(msg, 0, rctx.WorkspaceServiceFromContext(ctx))
	}
	raw, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	event := agentkit.SessionEvent{
		AgentID: agentID,
		Type:    typ,
		Data:    raw,
	}
	if logicalChars > 0 && logicalChars > derive.EstimateLogicalChars(msg) {
		event.Metadata = map[string]any{MetadataLogicalChars: logicalChars}
	}
	// Attribute user turns to whoever sent them. Only user messages carry this:
	// stamping the assistant with the user who prompted it would make the reply
	// look like that person's words on replay.
	if typ == agentkit.EventUserMessage {
		event.UserID = rctx.UserIDFromContext(ctx)
		if md := rctx.MetadataFromContext(ctx); len(md) > 0 {
			if event.Metadata == nil {
				event.Metadata = make(map[string]any, len(md))
			}
			for k, v := range md {
				event.Metadata[k] = v
			}
		}
	}
	_, err = s.Append(ctx, event)
	return err
}

// AppendToolCall sanitizes and appends a tool call event.
func AppendToolCall(ctx context.Context, s agentkit.Session, agentID agentkit.AgentID, call agentkit.ToolCall) error {
	call = derive.SanitizeToolCall(call)
	raw, err := json.Marshal(call)
	if err != nil {
		return err
	}
	_, err = s.Append(ctx, agentkit.SessionEvent{
		AgentID: agentID,
		Type:    agentkit.EventToolCall,
		Data:    raw,
	})
	return err
}

// AppendToolResult appends a tool result event.
func AppendToolResult(ctx context.Context, s agentkit.Session, agentID agentkit.AgentID, result agentkit.ToolResult) error {
	raw, err := json.Marshal(result)
	if err != nil {
		return err
	}
	_, err = s.Append(ctx, agentkit.SessionEvent{
		AgentID: agentID,
		Type:    agentkit.EventToolResult,
		Data:    raw,
	})
	return err
}
