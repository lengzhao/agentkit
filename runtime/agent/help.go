package agent

import "github.com/lengzhao/agentkit/plugindoc"

func init() {
	plugindoc.Register("agent/coding", plugindoc.Doc{
		Summary: "Default coding agent: runs one turn against session, LLM, tools and prompt.",
		ConfigNotes: map[string]string{
			"id":       "agent id, referenced by loop.defaultAgent",
			"model":    "model name passed to the LLM provider",
			"maxSteps": "steps allowed in one segment",
			"retry":    "per-step retry for transient provider failures",
			"budget":   "hard bounds for a whole turn including its continuations. No hook can extend a turn past these",
		},
		BestPractices: []string{
			"budget.maxContinuations defaults to 0, i.e. one segment: an agent stays request/response until you raise it.",
			"budget.softRatio (default 0.8) marks the point where a turn-stopping hook should wrap up rather than start new work.",
			"An interrupted turn is repaired on the next turn, so a crash mid-tool-call does not leave the session unusable.",
		},
	})
}
