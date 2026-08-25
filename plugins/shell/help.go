package shell

import "github.com/lengzhao/agentkit/plugindoc"

func init() {
	plugindoc.Register("shell/bash", plugindoc.Doc{
		Summary: "Run bash commands with a per-command timeout, rooted in the workspace.",
		ConfigNotes: map[string]string{
			"workDir":        "working directory relative to the workspace root",
			"timeoutSeconds": "per-command limit; 0 falls back to the built-in default",
		},
	})
}
