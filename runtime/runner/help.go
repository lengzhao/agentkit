package runner

import "github.com/lengzhao/agentkit/plugindoc"

func init() {
	plugindoc.Register("runner", plugindoc.Doc{
		Summary: "Root plugin: connects Platform to Loop and owns process lifecycle.",
		ConfigNotes: map[string]string{
			"shutdownTimeoutSeconds": "how long shutdown waits for in-flight turns to record turn/end; 0 waits indefinitely",
			"maxConcurrentTurns":     "turns running at once, default 1. Turns from different sessions share one workspace, so raise it only for transports whose sessions are genuinely independent",
		},
		BestPractices: []string{
			"Ordering within a session is preserved at any concurrency; only cross-session turns overlap.",
			"A panicking or failing turn is reported on its session and never kills the process.",
		},
	})
}
