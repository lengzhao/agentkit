package loop

import (
	"context"
	"strings"

	"github.com/lengzhao/agentkit"
)

// agentTraceModel returns the model id recorded in turn trace metadata.
// It prefers the session-effective model (session/global override applied)
// over the agent's configured default, so telemetry reflects the model the
// turn will actually use.
func agentTraceModel(ctx context.Context, ag agentkit.Agent) string {
	if ag == nil {
		return ""
	}
	if em, ok := ag.(interface{ EffectiveModel(context.Context) string }); ok {
		if m := strings.TrimSpace(em.EffectiveModel(ctx)); m != "" {
			return m
		}
	}
	if cm, ok := ag.(interface{ ConfiguredModel() string }); ok {
		return strings.TrimSpace(cm.ConfiguredModel())
	}
	return ""
}
