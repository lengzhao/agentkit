package hooks

import "github.com/lengzhao/agentkit/plugindoc"

func init() {
	plugindoc.Register("hooks/runtime", plugindoc.Doc{
		Summary: "Collect HookProvider instances and run the typed hook chains.",
		BestPractices: []string{
			"Chain order follows the providers dep order, and the first error aborts the chain.",
		},
	})
}
