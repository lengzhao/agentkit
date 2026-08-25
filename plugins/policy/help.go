package policy

import "github.com/lengzhao/agentkit/plugindoc"

func init() {
	plugindoc.Register("policy/deny-dangerous-shell", plugindoc.Doc{
		Summary: "Deny bash commands matching known destructive patterns (rm -rf /).",
		BestPractices: []string{
			"A backstop, not a sandbox: keep it on, but do not rely on it alone.",
		},
	})
	plugindoc.Register("policy/shell-allowlist", plugindoc.Doc{
		Summary: "Gate shell commands by command prefix.",
		ConfigNotes: map[string]string{
			"allow":  "command prefixes that may run, e.g. \"go test\", \"git status\"",
			"deny":   "command prefixes that never run; checked before allow",
			"strict": "deny anything outside allow. Without it, unlisted commands fall through to ask, so the approval provider still gets a say",
			"tool":   "shell tool name to guard; defaults to \"bash\"",
		},
		BestPractices: []string{
			"Chained commands are checked segment by segment regardless of strict, so `git status && rm -rf /` cannot ride in on an allowed prefix.",
			"Under an unattended run this replaces human judgement: prefer strict with a narrow allow list over approval/auto-allow on its own.",
		},
	})
	plugindoc.Register("policy/path-denylist", plugindoc.Doc{
		Summary: "Deny tool calls whose path argument matches a glob.",
		ConfigNotes: map[string]string{
			"deny":  "glob patterns matched against the path argument; ** spans directories. Empty falls back to DefaultDeniedPaths (.git, .env, .ssh, *.pem)",
			"tools": "limit enforcement to these tool names; empty means every tool taking a path",
		},
		BestPractices: []string{
			"Mandatory alongside approval/auto-allow, which does no filtering of its own.",
		},
	})
}
