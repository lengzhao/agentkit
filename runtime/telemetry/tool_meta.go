package telemetry

import (
	"encoding/json"
	"strings"

	"github.com/lengzhao/agentkit"
)

const maxMetaFieldRunes = 512

// ToolObservationAttrs extracts filter-friendly metadata from a tool call.
func ToolObservationAttrs(call agentkit.ToolCall) map[string]string {
	out := map[string]string{
		"tool_call_id": string(call.ID),
	}
	switch call.Name {
	case "read":
		var in struct {
			Path string `json:"path"`
		}
		if json.Unmarshal(call.Input, &in) == nil {
			if p := strings.TrimSpace(in.Path); p != "" {
				out["read_path"] = p
			}
		}
	case "delegate":
		var in struct {
			Agent string `json:"agent"`
			Task  string `json:"task"`
			Async *bool  `json:"async"`
		}
		if json.Unmarshal(call.Input, &in) == nil {
			if a := strings.TrimSpace(in.Agent); a != "" {
				out["delegate_agent"] = a
			}
			if t := strings.TrimSpace(in.Task); t != "" {
				out["delegate_task"] = truncateMeta(t)
			}
			if in.Async != nil {
				out["delegate_async"] = boolString(*in.Async)
			}
		}
	}
	return out
}

func truncateMeta(s string) string {
	if s == "" {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= maxMetaFieldRunes {
		return s
	}
	return string(runes[:maxMetaFieldRunes]) + "…"
}

func boolString(v bool) string {
	if v {
		return "true"
	}
	return "false"
}
