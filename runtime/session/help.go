package session

import "github.com/lengzhao/agentkit/plugindoc"

func init() {
	plugindoc.Register("session/memory", plugindoc.Doc{
		Summary: "Ephemeral in-memory session. Nothing survives the process.",
		ConfigNotes: map[string]string{
			"id":                 "fixed session id",
			"maxToolResultBytes": "truncate tool results as they are appended",
		},
	})
	plugindoc.Register("session/jsonl", plugindoc.Doc{
		Summary: "Append-only JSONL event log for one session.",
		ConfigNotes: map[string]string{
			"path": "the log file itself, not a directory",
			"id":   "fixed session id",
		},
		BestPractices: []string{
			"Reopening a file resumes event sequence numbering, so seq stays unique across restarts.",
		},
	})
	plugindoc.Register("session/store", plugindoc.Doc{
		Summary: "Resolve durable JSONL sessions by id; contributes /new and /session.",
		ConfigNotes: map[string]string{
			"dir": "root directory holding one file per session, resolved through the workspace",
		},
	})
	plugindoc.Register("session/static", plugindoc.Doc{
		Summary: "Wrap one pre-built Session as a store that returns it for every id.",
		BestPractices: []string{
			"For tests and single-session hosts; every session id maps to the same log.",
		},
	})
}
