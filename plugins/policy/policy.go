package policy

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/pluginkit"
)

func init() {
	pluginkit.Register("policy/deny-dangerous-shell", New)
	pluginkit.Register("policy/shell-allowlist", NewShellAllowlist)
	pluginkit.Register("policy/path-denylist", NewPathDenylist)
}

// New registers policy/deny-dangerous-shell: Deny bash commands matching known destructive patterns (rm -rf /).
//
// Best practices:
//   - A backstop, not a sandbox: keep it on, but do not rely on it alone.
func New() (agentkit.Policy, error) {
	return agentkit.PolicyFunc(func(_ context.Context, in agentkit.PolicyInput) agentkit.Decision {
		if in.ToolCall == nil || in.ToolCall.Name != "bash" {
			return agentkit.Allow()
		}
		var args struct {
			Command string `json:"command"`
		}
		if err := json.Unmarshal(in.ToolCall.Input, &args); err != nil {
			return agentkit.Deny("invalid shell arguments")
		}
		cmd := strings.TrimSpace(strings.ToLower(args.Command))
		if strings.Contains(cmd, "rm -rf /") || strings.Contains(cmd, "rm -rf /*") {
			return agentkit.Deny("dangerous shell command")
		}
		return agentkit.Allow()
	}), nil
}
