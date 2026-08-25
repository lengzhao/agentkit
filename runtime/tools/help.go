package tools

import "github.com/lengzhao/agentkit/plugindoc"

func init() {
	plugindoc.Register("tools/runtime", plugindoc.Doc{
		Summary: "Tool orchestration: visibility, policy evaluation, hooks, execution, result capping.",
		ConfigNotes: map[string]string{
			"defaultTimeoutSeconds": "per-call timeout when a tool has no specific entry",
			"maxResultBytes":        "truncation limit applied to every tool result",
			"toolTimeouts":          "per-tool timeout overrides, keyed by tool name",
		},
		BestPractices: []string{
			"Policies are the enforcement plane; hooks only observe and rewrite. Put a security rule in a policy.",
			"The approval dep is consulted only for ask decisions, so it never sees an allowed or denied call.",
		},
	})
}
