package session

import (
	"context"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/rctx"
)

const (
	MetadataSessionScope   = rctx.MetadataSessionScope
	MetadataConversationID = rctx.MetadataConversationID
	MetadataTurnCount      = rctx.MetadataTurnCount
)

// WithMetadataScope stamps session scope onto envelope metadata for /new entry-key resolution.
//
// Deprecated: use rctx.WithMetadataScope.
func WithMetadataScope(env agentkit.TurnEnvelope, scope SessionScope) agentkit.TurnEnvelope {
	return rctx.WithMetadataScope(env, scope)
}

// MergeEnvelopeMetadata copies extra metadata onto env.
//
// Deprecated: use rctx.MergeEnvelopeMetadata.
func MergeEnvelopeMetadata(env agentkit.TurnEnvelope, extra map[string]any) agentkit.TurnEnvelope {
	return rctx.MergeEnvelopeMetadata(env, extra)
}

// SessionScopeFromContext reads the runner/platform session scope from envelope metadata.
//
// Deprecated: use rctx.SessionScopeFromContext.
func SessionScopeFromContext(ctx context.Context) SessionScope {
	return rctx.SessionScopeFromContext(ctx)
}

// MetadataString returns a trimmed metadata string when present.
//
// Deprecated: use rctx.MetadataString.
func MetadataString(env agentkit.TurnEnvelope, key string) string {
	return rctx.MetadataString(env, key)
}
