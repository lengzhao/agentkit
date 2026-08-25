package workspace

import "github.com/lengzhao/agentkit/plugindoc"

func init() {
	plugindoc.Register("workspace/default", plugindoc.Doc{
		Summary: "Dual-root workspace: a global home and a local .agentkit directory under the project.",
		ConfigNotes: map[string]string{
			"global": "global root, conventionally ~/.agentkit",
			"local":  "local root, default .agentkit under cwd",
			"scope":  "which root an unprefixed path resolves against: global or local",
			"root":   "single-root shorthand, kept for older configs; prefer global and local",
		},
		BestPractices: []string{
			"Prefix a path with global: or local: to pin it regardless of scope.",
		},
	})
}
