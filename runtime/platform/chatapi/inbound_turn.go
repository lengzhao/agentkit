package chatapi

import (
	"strings"

	"github.com/lengzhao/agentkit"
)

// mergeInboundTurnMetadata copies metadata and stamps the chat-api run id as the
// loop turn id so outbound events correlate via OutboundEvent.TurnID.
func mergeInboundTurnMetadata(metadata map[string]any, runID string) map[string]any {
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return metadata
	}
	out := make(map[string]any, len(metadata)+1)
	for k, v := range metadata {
		out[k] = v
	}
	out[agentkit.MetadataTurnID] = runID
	return out
}
