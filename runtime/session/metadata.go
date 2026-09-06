package session

import (
	"context"
	"fmt"
	"strings"

	"github.com/lengzhao/agentkit"
)

// Envelope metadata keys for slash commands and platform extensions.
const (
	MetadataSessionScope   = "sessionScope"
	MetadataConversationID = "conversationId"
	MetadataTurnCount      = "turnCount"
)

// WithMetadataScope stamps session scope onto envelope metadata for /new entry-key resolution.
func WithMetadataScope(env agentkit.TurnEnvelope, scope SessionScope) agentkit.TurnEnvelope {
	if scope == "" {
		scope = DefaultSessionScope
	}
	return mergeEnvelopeMetadata(env, map[string]any{
		MetadataSessionScope: string(scope),
	})
}

// MergeEnvelopeMetadata copies extra metadata onto env.
func MergeEnvelopeMetadata(env agentkit.TurnEnvelope, extra map[string]any) agentkit.TurnEnvelope {
	return mergeEnvelopeMetadata(env, extra)
}

func mergeEnvelopeMetadata(env agentkit.TurnEnvelope, extra map[string]any) agentkit.TurnEnvelope {
	if len(extra) == 0 {
		return env
	}
	meta := env.Metadata
	if meta == nil {
		meta = make(map[string]any, len(extra))
	}
	for k, v := range extra {
		meta[k] = v
	}
	env.Metadata = meta
	return env
}

// SessionScopeFromContext reads the runner/platform session scope from envelope metadata.
func SessionScopeFromContext(ctx context.Context) SessionScope {
	env := EnvelopeFromContext(ctx)
	if env.Metadata != nil {
		if raw, ok := env.Metadata[MetadataSessionScope]; ok {
			switch v := raw.(type) {
			case SessionScope:
				if v != "" {
					return v
				}
			case string:
				if scope := ParseScope(v); scope != "" {
					return scope
				}
			}
		}
	}
	return DefaultSessionScope
}

// MetadataString returns a trimmed metadata string when present.
func MetadataString(env agentkit.TurnEnvelope, key string) string {
	if env.Metadata == nil {
		return ""
	}
	raw, ok := env.Metadata[key]
	if !ok || raw == nil {
		return ""
	}
	switch v := raw.(type) {
	case string:
		return strings.TrimSpace(v)
	default:
		return strings.TrimSpace(fmt.Sprint(v))
	}
}
