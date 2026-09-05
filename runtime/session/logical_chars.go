package session

import (
	"encoding/json"

	"github.com/lengzhao/agentkit"
	rtmedia "github.com/lengzhao/agentkit/runtime/media"
)

const visionPlaceholderChars = 256

// EstimateLogicalChars approximates model-visible size before session storage.
func EstimateLogicalChars(msg agentkit.ModelMessage) int {
	chars := len(msg.Role)
	chars += partsLogicalChars(msg.Content)
	for _, call := range msg.ToolCalls {
		chars += len(call.Name) + len(call.Input)
	}
	for _, result := range msg.ToolResults {
		chars += len(result.Name) + len(result.Content)
	}
	return chars
}

func partsLogicalChars(parts []agentkit.ContentPart) int {
	chars := 0
	for _, part := range parts {
		chars += len(part.Text) + len(part.Source) + len(part.URL)
		switch part.Type {
		case "image", "image_url":
			if isDataURL(part.URL) {
				chars += visionPlaceholderChars
			}
		case rtmedia.ContentTypeAttachmentRef:
			if part.URL != "" && !isDataURL(part.URL) {
				chars += len(part.URL)
			}
		}
	}
	return chars
}

// SumLogicalCharsFromEvents totals logical message size for compaction estimates.
func SumLogicalCharsFromEvents(events []agentkit.SessionEvent, agentID agentkit.AgentID) int {
	total := 0
	for _, ev := range events {
		if ev.Type != agentkit.EventUserMessage && ev.Type != agentkit.EventAssistantMessage {
			continue
		}
		if agentID != "" && ev.AgentID != agentID {
			continue
		}
		if v := metadataInt(ev.Metadata, MetadataLogicalChars); v > 0 {
			total += v
			continue
		}
		var msg agentkit.ModelMessage
		if err := json.Unmarshal(ev.Data, &msg); err != nil {
			continue
		}
		total += EstimateLogicalChars(msg)
	}
	return total
}

func metadataInt(md map[string]any, key string) int {
	if len(md) == 0 {
		return 0
	}
	raw, ok := md[key]
	if !ok {
		return 0
	}
	switch v := raw.(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	default:
		return 0
	}
}
