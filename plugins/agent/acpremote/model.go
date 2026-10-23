package acpremote

import (
	"context"
	"strings"

	acp "github.com/coder/acp-go-sdk"
	"github.com/lengzhao/agentkit"
	capacp "github.com/lengzhao/agentkit/cap/acp"
)

// modelFromConfig uses the semantic category, with the conventional option id
// as a fallback for agents that omit the optional category.
func modelFromConfig(options []acp.SessionConfigOption) string {
	for _, option := range options {
		s := option.Select
		if s == nil {
			continue
		}
		if (s.Category != nil && *s.Category == acp.SessionConfigOptionCategoryModel) ||
			(s.Category == nil && s.Id == "model") {
			return strings.TrimSpace(string(s.CurrentValue))
		}
	}
	return ""
}

func (e *updateEmitter) emitModel(model string) error {
	e.modelMu.Lock()
	defer e.modelMu.Unlock()
	if model == e.model {
		return nil
	}
	e.model = model
	if model == "" {
		return nil
	}
	if err := e.ensureStarted(); err != nil {
		return err
	}
	text := "[model: " + model + "]\n"
	e.thought.WriteString(text)
	return e.emitDelta(agentkit.AssistantEventThinkingDelta, 1, text)
}

// Keep restore response metadata without changing the shared restorer contract.
type modelRestoreConn struct {
	capacp.SessionRestoreConn
	proc *subprocess
}

func (c modelRestoreConn) LoadSession(ctx context.Context, req acp.LoadSessionRequest) (acp.LoadSessionResponse, error) {
	resp, err := c.SessionRestoreConn.LoadSession(ctx, req)
	if err == nil {
		c.proc.models.Store(req.SessionId, modelFromConfig(resp.ConfigOptions))
	}
	return resp, err
}

func (c modelRestoreConn) ResumeSession(ctx context.Context, req acp.ResumeSessionRequest) (acp.ResumeSessionResponse, error) {
	resp, err := c.SessionRestoreConn.ResumeSession(ctx, req)
	if err == nil {
		c.proc.models.Store(req.SessionId, modelFromConfig(resp.ConfigOptions))
	}
	return resp, err
}
