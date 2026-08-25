package credentials

import "github.com/lengzhao/agentkit/plugindoc"

func init() {
	plugindoc.Register("credentials/env", plugindoc.Doc{
		Summary: "Resolve secrets from environment variables.",
		ConfigNotes: map[string]string{
			"prefix": "prepended to every lookup key",
		},
		BestPractices: []string{
			"Reference a secret as env:NAME from the consumer's apiKeyRef rather than inlining it in YAML.",
		},
	})
}
