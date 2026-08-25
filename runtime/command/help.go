package command

import "github.com/lengzhao/agentkit/plugindoc"

func init() {
	plugindoc.Register("commands/registry", plugindoc.Doc{
		Summary: "Aggregate slash commands contributed by CommandProvider plugins.",
		ConfigNotes: map[string]string{
			"allow": "expose only these command names; empty means all",
			"deny":  "hide these command names; applied after allow",
		},
		BestPractices: []string{
			"Providers are discovered from the built graph, so a command appears as soon as its plugin is wired in.",
		},
	})
}
