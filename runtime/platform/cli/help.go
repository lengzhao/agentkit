package cli

import "github.com/lengzhao/agentkit/plugindoc"

func init() {
	plugindoc.Register("platform/cli", plugindoc.Doc{
		Summary: "Interactive terminal platform with slash commands.",
		ConfigNotes: map[string]string{
			"prompt":           "first message; falls back to the positional command-line arguments",
			"once":             "run a single turn and exit instead of looping on stdin",
			"defaultSessionId": "session to attach to; defaults to cli:default so a restart resumes the conversation",
		},
	})
}
