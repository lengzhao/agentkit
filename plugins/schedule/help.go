package schedule

import "github.com/lengzhao/agentkit/plugindoc"

func init() {
	plugindoc.Register("schedule/file", plugindoc.Doc{
		Summary: "Durable cron job table, shared by platform/worker and tool/schedule.",
		ConfigNotes: map[string]string{
			"path": "JSON file holding the jobs, resolved through the workspace",
		},
		BestPractices: []string{
			"Point the worker and tool/schedule at one instance, or the agent will schedule jobs nothing fires.",
			"Jobs carry a source: config jobs are reconciled against the preset on every start, agent jobs are left alone.",
			"Writes go through a temp file and rename, so a process killed mid-write leaves no truncated table.",
		},
	})
}
