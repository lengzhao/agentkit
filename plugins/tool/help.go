package tool

import "github.com/lengzhao/agentkit/plugindoc"

func init() {
	plugindoc.Register("tool/read-file", plugindoc.Doc{
		Summary: "Read a text file from the workspace.",
		ConfigNotes: map[string]string{
			"maxBytes": "truncation limit; defaults to 1 MiB",
		},
		BestPractices: []string{
			"Prefer grep for searching inside files instead of reading whole trees.",
		},
	})
	plugindoc.Register("tool/write-file", plugindoc.Doc{
		Summary: "Write full file contents, creating or overwriting the target path.",
		BestPractices: []string{
			"Prefer edit-file for small, targeted changes to an existing file.",
		},
	})
	plugindoc.Register("tool/edit-file", plugindoc.Doc{
		Summary: "Apply exact search-and-replace edits to an existing file.",
		BestPractices: []string{
			"oldText must match exactly, including whitespace and indentation.",
			"Batch related edits into one call when they touch the same file.",
		},
	})
	plugindoc.Register("tool/grep", plugindoc.Doc{
		Summary: "Search file contents with a regular expression.",
		ConfigNotes: map[string]string{
			"maxMatches": "cap on matches returned per call",
		},
		BestPractices: []string{
			"Scope the search path to avoid scanning the whole repository.",
		},
	})
	plugindoc.Register("tool/find", plugindoc.Doc{
		Summary: "Find files by filename glob under a directory.",
		ConfigNotes: map[string]string{
			"maxResults": "cap on paths returned per call",
		},
		BestPractices: []string{
			"Patterns match a single path segment; ** recursive glob is not supported.",
		},
	})
	plugindoc.Register("tool/list-dir", plugindoc.Doc{
		Summary: "List entries in a workspace directory.",
	})
	plugindoc.Register("tool/shell", plugindoc.Doc{
		Summary: "Execute a shell command through the configured executor.",
		BestPractices: []string{
			"Keep commands non-interactive; avoid pagers and prompts.",
			"For unattended runs pair with policy/shell-allowlist in strict mode.",
			"The per-command timeout comes from the shell dep, not from this tool.",
		},
	})
	plugindoc.Register("tool/skill", plugindoc.Doc{
		Summary: "Discover and load an agent skill by name.",
		BestPractices: []string{
			"Load a skill once per task, then follow its instructions.",
		},
	})
	plugindoc.Register("tool/todo", plugindoc.Doc{
		Summary: "Durable task list; the signal an autonomous run uses to decide whether work remains.",
		BestPractices: []string{
			"op=set replaces the whole list, op=complete closes ids, op=list reads it.",
			"Pair with tool/finish: an empty pending list alone does not end a run.",
		},
	})
	plugindoc.Register("tool/finish", plugindoc.Doc{
		Summary: "End an autonomous run, with status=completed or status=blocked.",
		BestPractices: []string{
			"This is the only signal that stops a run early; otherwise it runs to budget.",
		},
	})
	plugindoc.Register("tool/schedule", plugindoc.Doc{
		Summary: "Let the agent list, add and remove its own cron jobs.",
		ConfigNotes: map[string]string{
			"maxJobs": "cap on agent-created jobs, default 32; jobs declared in config do not count against it",
		},
		BestPractices: []string{
			"Ids are assigned by the registry (agent-1, agent-2, ...); read them back with op=list before op=remove.",
			"Needs a schedule registry that platform/worker also uses, or nothing will fire the jobs.",
		},
	})
}
