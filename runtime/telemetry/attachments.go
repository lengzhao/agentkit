package telemetry

import (
	"strings"

	"github.com/lengzhao/agentkit"
)

// AttachmentSources collects workspace paths from message content (Source fields).
func AttachmentSources(msg agentkit.ModelMessage) []string {
	var out []string
	seen := make(map[string]struct{})
	for _, part := range msg.Content {
		src := strings.TrimSpace(part.Source)
		if src == "" {
			continue
		}
		if _, ok := seen[src]; ok {
			continue
		}
		seen[src] = struct{}{}
		out = append(out, src)
	}
	return out
}

// JoinAttachmentSources formats paths for trace metadata.
func JoinAttachmentSources(paths []string) string {
	if len(paths) == 0 {
		return ""
	}
	return strings.Join(paths, ",")
}
