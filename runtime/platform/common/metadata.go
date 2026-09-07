package common

import (
	"strings"

	"github.com/lengzhao/agentkit"
)

// UserProfileMetadata builds inbound sender metadata for runner inject.
// Leaf platforms query user APIs and set displayName/email; chat-api maps
// caller headers into Metadata instead.
func UserProfileMetadata(name, email string) map[string]any {
	md := make(map[string]any, 2)
	if name = strings.TrimSpace(name); name != "" {
		md["displayName"] = name
	}
	if email = strings.TrimSpace(email); email != "" {
		md["email"] = email
	}
	if len(md) == 0 {
		return nil
	}
	return md
}

// WithMetadata merges metadata onto an inbound event without overwriting existing keys.
func WithMetadata(event agentkit.MessageEvent, metadata map[string]any) agentkit.MessageEvent {
	if len(metadata) == 0 {
		return event
	}
	if event.Metadata == nil {
		event.Metadata = metadata
		return event
	}
	for k, v := range metadata {
		if _, ok := event.Metadata[k]; !ok {
			event.Metadata[k] = v
		}
	}
	return event
}
