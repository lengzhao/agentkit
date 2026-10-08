package loop

import (
	"strings"

	"github.com/lengzhao/agentkit"
)

func agentConfiguredModel(ag agentkit.Agent) string {
	if ag == nil {
		return ""
	}
	if cm, ok := ag.(interface{ ConfiguredModel() string }); ok {
		return strings.TrimSpace(cm.ConfiguredModel())
	}
	return ""
}
