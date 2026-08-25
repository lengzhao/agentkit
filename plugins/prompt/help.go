package prompt

import "github.com/lengzhao/agentkit/plugindoc"

func init() {
	plugindoc.Register("prompt/section/agents-md", plugindoc.Doc{
		Summary: "Inject AGENTS.md instructions discovered in the workspace hierarchy.",
		ConfigNotes: map[string]string{
			"root": "directory to start the upward search from",
		},
	})
	plugindoc.Register("prompt/section/skills", plugindoc.Doc{
		Summary: "Inject a catalog of available skills so the model knows what it can load.",
	})
	plugindoc.Register("prompt/section/static", plugindoc.Doc{
		Summary: "Inject a fixed block of system prompt text from config.",
		ConfigNotes: map[string]string{
			"name":    "section label, used for ordering and debugging",
			"content": "the prompt text",
		},
	})
}
