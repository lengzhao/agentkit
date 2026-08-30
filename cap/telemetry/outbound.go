package telemetry

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/permission"
)

const turnUserVisibleSep = "-------------\n"

// WrapOutboundEmit records user-visible outbound events on the active turn trace.
func WrapOutboundEmit(ctx context.Context, emit agentkit.OutboundEmit) agentkit.OutboundEmit {
	if emit == nil {
		return nil
	}
	return func(ctx context.Context, event agentkit.OutboundEvent) error {
		recordTurnOutbound(ctx, event)
		return emit(ctx, event)
	}
}

func recordTurnOutbound(ctx context.Context, event agentkit.OutboundEvent) {
	switch event.Type {
	case agentkit.EventMessageEnd:
		var payload agentkit.MessageEndPayload
		if err := json.Unmarshal(event.Data, &payload); err != nil {
			return
		}
		// Only the final assistant reply reaches the user; steps with tool calls
		// are internal and already visible in the SSE/tool trace.
		if len(payload.Message.ToolCalls) > 0 {
			return
		}
		if text := userVisibleText(payload.Message); text != "" {
			appendTurnUserVisible(ctx, text)
		}
	case agentkit.EventAssistantMessage:
		var msg agentkit.ModelMessage
		if err := json.Unmarshal(event.Data, &msg); err != nil {
			return
		}
		if text := userVisibleText(msg); text != "" {
			appendTurnUserVisible(ctx, "[send] "+text)
		}
	case agentkit.EventPermissionRequest:
		var payload permission.RequestPayload
		if err := json.Unmarshal(event.Data, &payload); err != nil {
			return
		}
		if payload.Kind != permission.KindQuestion || payload.Question == nil {
			return
		}
		if text := formatAskUser(payload.Question); text != "" {
			appendTurnUserVisible(ctx, text)
		}
	}
}

func userVisibleText(msg agentkit.ModelMessage) string {
	if text := textFromParts(msg.Content); text != "" {
		return text
	}
	var files []string
	for _, part := range msg.Content {
		switch part.Type {
		case "image", "document":
			if part.URL != "" {
				files = append(files, part.URL)
			}
		}
	}
	if len(files) == 0 {
		return ""
	}
	return "[file] " + strings.Join(files, ", ")
}

func formatAskUser(q *permission.Question) string {
	prompt := strings.TrimSpace(q.Prompt)
	if prompt == "" {
		return ""
	}
	var b stringsBuilder
	b.WriteString("[ask_user] ")
	b.WriteString(prompt)
	if len(q.Options) > 0 {
		opts := make([]string, 0, len(q.Options))
		for _, opt := range q.Options {
			if label := strings.TrimSpace(opt.Label); label != "" {
				opts = append(opts, label)
			}
		}
		if len(opts) > 0 {
			b.WriteString("\noptions: ")
			b.WriteString(strings.Join(opts, ", "))
		}
	}
	return b.String()
}
