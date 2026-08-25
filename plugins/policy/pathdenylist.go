package policy

import (
	"context"
	"encoding/json"
	"path"
	"strings"

	"github.com/lengzhao/agentkit"
)

type PathDenylistConfig struct {
	// Deny holds glob patterns matched against the path argument; ** spans directories. Empty falls back to DefaultDeniedPaths (.git, .env, .ssh, *.pem).
	Deny []string `json:"deny"`
	// Tools limits enforcement to these tool names; empty means every tool taking a path.
	Tools []string `json:"tools"`
}

// DefaultDeniedPaths protects the things an unattended run must never touch:
// version control internals, credentials, and SSH keys.
var DefaultDeniedPaths = []string{
	".git/**",
	"**/.git/**",
	".env",
	"**/.env",
	"**/.env.*",
	".ssh/**",
	"**/.ssh/**",
	"**/id_rsa",
	"**/*.pem",
}

// NewPathDenylist registers policy/path-denylist: Deny tool calls whose path argument matches a glob.
//
// Best practices:
//   - Mandatory alongside approval/auto-allow, which does no filtering of its own.
func NewPathDenylist(cfg PathDenylistConfig) (agentkit.Policy, error) {
	deny := cfg.Deny
	if len(deny) == 0 {
		deny = DefaultDeniedPaths
	}
	tools := make(map[string]bool, len(cfg.Tools))
	for _, name := range cfg.Tools {
		tools[strings.TrimSpace(name)] = true
	}
	return agentkit.PolicyFunc(func(_ context.Context, in agentkit.PolicyInput) agentkit.Decision {
		if in.ToolCall == nil {
			return agentkit.Allow()
		}
		if len(tools) > 0 && !tools[in.ToolCall.Name] {
			return agentkit.Allow()
		}
		var args struct {
			Path string `json:"path"`
		}
		if err := json.Unmarshal(in.ToolCall.Input, &args); err != nil {
			// Inputs without a decodable path are not this policy's concern; the
			// tool itself rejects malformed arguments.
			return agentkit.Allow()
		}
		if args.Path == "" {
			return agentkit.Allow()
		}
		if pattern, ok := matchDeniedPath(args.Path, deny); ok {
			return agentkit.Decision{
				Kind:   agentkit.DecisionDeny,
				Reason: "path denied by policy: " + args.Path + " matches " + pattern,
				Audit:  map[string]string{"policy": "path-denylist", "pattern": pattern},
			}
		}
		return agentkit.Allow()
	}), nil
}

func matchDeniedPath(target string, patterns []string) (string, bool) {
	cleaned := normalizePolicyPath(target)
	for _, pattern := range patterns {
		if globMatch(pattern, cleaned) {
			return pattern, true
		}
	}
	return "", false
}

func normalizePolicyPath(target string) string {
	cleaned := path.Clean(strings.ReplaceAll(strings.TrimSpace(target), "\\", "/"))
	return strings.TrimPrefix(cleaned, "./")
}

// globMatch supports `*` within one segment and `**` across segments, which is
// what the deny patterns need; path.Match alone cannot cross separators.
func globMatch(pattern, target string) bool {
	patternParts := strings.Split(pattern, "/")
	targetParts := strings.Split(target, "/")
	return matchSegments(patternParts, targetParts)
}

func matchSegments(pattern, target []string) bool {
	if len(pattern) == 0 {
		return len(target) == 0
	}
	if pattern[0] == "**" {
		// `**` matches zero or more segments; try every split point.
		for i := 0; i <= len(target); i++ {
			if matchSegments(pattern[1:], target[i:]) {
				return true
			}
		}
		return false
	}
	if len(target) == 0 {
		return false
	}
	ok, err := path.Match(pattern[0], target[0])
	if err != nil || !ok {
		return false
	}
	return matchSegments(pattern[1:], target[1:])
}
