package multiplex

import "github.com/lengzhao/agentkit/plugindoc"

func init() {
	plugindoc.Register("platform/multiplex", plugindoc.Doc{
		Summary: "Merge several platforms into one Runner entrypoint.",
		ConfigNotes: map[string]string{
			"names": "labels for the platforms dep, positionally matched; used in logs and PlatformID",
		},
		BestPractices: []string{
			"Raise runner.maxConcurrentTurns if the merged platforms should make progress in parallel.",
		},
	})
}
