package fs

import "github.com/lengzhao/agentkit/plugindoc"

func init() {
	plugindoc.Register("fs/local", plugindoc.Doc{
		Summary: "Local filesystem access, rooted inside the workspace.",
		ConfigNotes: map[string]string{
			"root": "directory relative to the workspace root; may use the global: or local: scope prefix",
		},
		BestPractices: []string{
			"Paths are resolved against the workspace, so a tool cannot escape root via ..",
		},
	})
	plugindoc.Register("fs/memory", plugindoc.Doc{
		Summary: "In-memory filesystem for tests and ephemeral sandboxes.",
		ConfigNotes: map[string]string{
			"files": "seed contents, keyed by path",
		},
	})
	plugindoc.Register("fs/readonly", plugindoc.Doc{
		Summary: "Read-only wrapper around another filesystem service.",
		BestPractices: []string{
			"Give read-only tools (read-file, grep, find, list-dir) this instead of fs/local.",
		},
	})
}
