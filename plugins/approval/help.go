package approval

import "github.com/lengzhao/agentkit/plugindoc"

func init() {
	plugindoc.Register("approval/cli", plugindoc.Doc{
		Summary: "Prompt on stderr for y/n before a gated tool call.",
		BestPractices: []string{
			"Blocks forever without a terminal; never use it in a worker or timer preset.",
		},
	})
	plugindoc.Register("approval/auto-deny", plugindoc.Doc{
		Summary: "Deny every ask decision. For tests and CI.",
	})
	plugindoc.Register("approval/auto-allow", plugindoc.Doc{
		Summary: "Allow every ask decision, for unattended runs.",
		ConfigNotes: map[string]string{
			"reason": "text recorded on each decision; defaults to \"auto-allowed: unattended run\"",
		},
		BestPractices: []string{
			"Never use alone. It filters nothing, so the Policy plane is the only enforcement left: pair it with policy/shell-allowlist and policy/path-denylist.",
			"Every decision is logged and audited, so an unattended run stays reviewable after the fact.",
		},
	})
}
