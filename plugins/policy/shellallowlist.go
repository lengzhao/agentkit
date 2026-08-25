package policy

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/lengzhao/agentkit"
)

type ShellAllowlistConfig struct {
	// Allow holds command prefixes that may run, e.g. "go test", "git status".
	Allow []string `json:"allow"`
	// Deny holds command prefixes that never run; checked before Allow.
	Deny []string `json:"deny"`
	// Strict denies anything outside Allow. Without it, unlisted commands fall through to ask, so the approval provider still gets a say.
	Strict bool `json:"strict"`
	// Tool is shell tool name to guard; defaults to "bash".
	Tool string `json:"tool"`
}

// NewShellAllowlist registers policy/shell-allowlist: Gate shell commands by command prefix.
//
// Best practices:
//   - Chained commands are checked segment by segment regardless of strict, so `git status && rm -rf /` cannot ride in on an allowed prefix.
//   - Under an unattended run this replaces human judgement: prefer strict with a narrow allow list over approval/auto-allow on its own.
func NewShellAllowlist(cfg ShellAllowlistConfig) (agentkit.Policy, error) {
	toolName := strings.TrimSpace(cfg.Tool)
	if toolName == "" {
		toolName = "bash"
	}
	allow := normalizePrefixes(cfg.Allow)
	deny := normalizePrefixes(cfg.Deny)
	return agentkit.PolicyFunc(func(_ context.Context, in agentkit.PolicyInput) agentkit.Decision {
		if in.ToolCall == nil || in.ToolCall.Name != toolName {
			return agentkit.Allow()
		}
		var args struct {
			Command string `json:"command"`
		}
		if err := json.Unmarshal(in.ToolCall.Input, &args); err != nil {
			return agentkit.Deny("invalid shell arguments")
		}
		command := strings.ToLower(strings.TrimSpace(args.Command))
		if command == "" {
			return agentkit.Deny("empty shell command")
		}
		if prefix, ok := matchPrefix(command, deny); ok {
			return agentkit.Decision{
				Kind:   agentkit.DecisionDeny,
				Reason: "shell command denied by policy: " + prefix,
				Audit:  map[string]string{"policy": "shell-allowlist", "denied_prefix": prefix},
			}
		}
		if prefix, ok := matchPrefix(command, allow); ok {
			return agentkit.Decision{
				Kind:  agentkit.DecisionAllow,
				Audit: map[string]string{"policy": "shell-allowlist", "allowed_prefix": prefix},
			}
		}
		// Chained commands can smuggle a denied step past a prefix match, so an
		// allowed prefix is required for the whole command, not just its head.
		if cfg.Strict {
			return agentkit.Decision{
				Kind:   agentkit.DecisionDeny,
				Reason: "shell command is not in the allowlist",
				Audit:  map[string]string{"policy": "shell-allowlist", "mode": "strict"},
			}
		}
		return agentkit.Ask("shell command is not in the allowlist")
	}), nil
}

func normalizePrefixes(in []string) []string {
	out := make([]string, 0, len(in))
	for _, prefix := range in {
		prefix = strings.ToLower(strings.TrimSpace(prefix))
		if prefix != "" {
			out = append(out, prefix)
		}
	}
	return out
}

// matchPrefix reports the matching prefix. Every segment of a chained command
// must match, so `git status && rm -rf /` cannot ride in on `git status`.
func matchPrefix(command string, prefixes []string) (string, bool) {
	if len(prefixes) == 0 {
		return "", false
	}
	segments := splitCommandChain(command)
	matched := ""
	for _, segment := range segments {
		found := ""
		for _, prefix := range prefixes {
			if segment == prefix || strings.HasPrefix(segment, prefix+" ") {
				found = prefix
				break
			}
		}
		if found == "" {
			return "", false
		}
		if matched == "" {
			matched = found
		}
	}
	return matched, matched != ""
}

// splitCommandChain breaks a command on shell operators so each part is checked.
func splitCommandChain(command string) []string {
	replacer := strings.NewReplacer("&&", "\n", "||", "\n", ";", "\n", "|", "\n")
	var out []string
	for _, part := range strings.Split(replacer.Replace(command), "\n") {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}
