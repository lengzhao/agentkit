package hook

import "github.com/lengzhao/agentkit/plugindoc"

func init() {
	plugindoc.Register("hook/before-step", plugindoc.Doc{
		Summary: "Run compaction services before each model step; contributes /compact.",
		BestPractices: []string{
			"Attach the same services here and to the agent's compaction dep: this hook covers the periodic check, the agent covers overflow recovery.",
		},
	})
	plugindoc.Register("hook/turn-continue", plugindoc.Doc{
		Summary: "Decide whether an autonomous turn continues or stops; contributes /status.",
		ConfigNotes: map[string]string{
			"maxContinuations": "segments this hook will ask for. The agent's budget.maxContinuations is the hard ceiling and always wins",
			"continuePrompt":   "text injected to start another segment",
			"wrapUpPrompt":     "injected instead of continuePrompt once the budget is softly exhausted",
			"requireFinish":    "keep going until tool/finish is called, even with no pending todos",
			"requireTodosDone": "keep going while todos are still pending",
			"stallLimit":       "stop after this many repeats of the same tool call signature",
		},
		BestPractices: []string{
			"Useless without tool/todo and tool/finish: with no completion signal it can only stop on budget or stall.",
			"Decision order is finish, then stall, then budget, then segment limit, then pending work.",
		},
	})
}
