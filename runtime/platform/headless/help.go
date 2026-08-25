package headless

import "github.com/lengzhao/agentkit/plugindoc"

func init() {
	plugindoc.Register("platform/worker", plugindoc.Doc{
		Summary: "Headless task runner: one-shot prompts, shell scripts, or a resident cron daemon.",
		ConfigNotes: map[string]string{
			"tasks":       "task list. A bare string is shorthand for {prompt}. A task with cron becomes a scheduled job instead of running once; prompt and script are mutually exclusive",
			"prompt":      "shorthand for a single-task tasks list",
			"sessionMode": "fresh (default, a new session per task) or fixed (all tasks share sessionId)",
			"sessionId":   "session prefix; required by sessionMode: fixed",
			"output":      "text or json",
			"stream":      "emit deltas as they arrive instead of only the final message",
			"pollSeconds": "how often cron mode re-checks the registry, default 30. This is also how long it takes to notice a job the agent just added",
		},
		BestPractices: []string{
			"Without any cron task the worker exits at EOF; with one it stays resident.",
			"A cron task needs the schedule dep, and a script task needs workspace and shell; both are checked at startup rather than silently skipped.",
			"Missed boundaries are skipped, not backfilled, so a restart does not replay a day of jobs.",
		},
	})
	plugindoc.Register("platform/timer", plugindoc.Doc{
		Summary: "Fire the same prompt on a fixed interval.",
		ConfigNotes: map[string]string{
			"everySeconds": "interval between ticks",
			"prompt":       "the message sent on every tick",
			"immediate":    "fire once at startup instead of waiting out the first interval",
			"maxRuns":      "stop after this many ticks; 0 runs until cancelled",
			"sessionMode":  "fresh (default) or fixed",
			"sessionId":    "session prefix; required by sessionMode: fixed",
			"output":       "text or json",
			"stream":       "emit deltas as they arrive instead of only the final message",
		},
		BestPractices: []string{
			"Ticks are anchored to the start time and missed ones are skipped, so a slow turn does not make the schedule drift.",
			"Use platform/worker with a cron expression when you need calendar times rather than an interval.",
		},
	})
}
