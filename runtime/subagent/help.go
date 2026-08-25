package subagent

import "github.com/lengzhao/agentkit/plugindoc"

func init() {
	plugindoc.Register("subagent/inprocess", plugindoc.Doc{
		Summary: "Run a child agent in-process from an agents/<name>.md definition and return only its conclusion.",
		ConfigNotes: map[string]string{
			"dirs":           "definition directories in precedence order; defaults to local:agents then global:agents",
			"maxSteps":       "step cap for definitions that do not set their own; defaults to 20",
			"timeoutSeconds": "wall clock for one delegation; 0 leaves the delegate tool's own timeout as the only bound",
		},
		BestPractices: []string{
			"deps.tools must be a sibling tools/runtime instance that does NOT mount tool/subagent: wiring the parent's runtime is a dependency cycle, and the separate instance is what makes 'only the main agent delegates' structural.",
			"Give the child a narrower tool set than the parent — read-only is the common case. Delegation is for context isolation, not for a second agent editing the same workspace.",
			"Pair with prompt/section/subagents so the parent can see who it may delegate to; the delegate tool's description is static and cannot list definitions read from disk.",
			"Raise the delegate entry in the parent's toolTimeouts: a child agent takes far longer than a normal tool call.",
		},
	})
}
