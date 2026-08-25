package prompt

import "github.com/lengzhao/agentkit/plugindoc"

func init() {
	plugindoc.Register("prompt/assembler/default", plugindoc.Doc{
		Summary: "Assemble SectionProvider contributions into one system prompt.",
		BestPractices: []string{
			"Sections are emitted in dep order, so put stable instructions before volatile context.",
		},
	})
}
