package agentkit

import "context"

// ModelBindStore reads and writes per-session LLM model overrides beside session logs.
// Missing bind means the agent uses its configured default model.
type ModelBindStore interface {
	ModelBind(ctx context.Context, id SessionID) (string, error)
	SetModelBind(ctx context.Context, id SessionID, model string) error
}
