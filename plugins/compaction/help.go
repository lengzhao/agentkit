package compaction

import "github.com/lengzhao/agentkit/plugindoc"

func init() {
	plugindoc.Register("compaction/summary", plugindoc.Doc{
		Summary: "Replace older messages with an LLM-written summary.",
		ConfigNotes: map[string]string{
			"minMessages":   "do nothing until the history reaches this many messages",
			"keepRecent":    "trailing messages left verbatim; a forced compaction with fewer messages than this is a no-op",
			"summaryModel":  "model used for the summary; defaults to the agent's model",
			"summaryPrompt": "overrides the built-in summarisation instruction",
		},
	})
	plugindoc.Register("compaction/prune-tool-results", plugindoc.Doc{
		Summary: "Trim verbose tool results without calling a model.",
		ConfigNotes: map[string]string{
			"maxToolResultBytes": "per-result truncation limit",
		},
		BestPractices: []string{
			"Cheap and lossless enough to run before compaction/summary in the same chain.",
		},
	})
	plugindoc.Register("compaction/token-limit", plugindoc.Doc{
		Summary: "Trigger inner compaction services once the context crosses a token threshold.",
		ConfigNotes: map[string]string{
			"maxTokens":     "absolute trigger; takes precedence over contextWindow",
			"contextWindow": "model context size; the trigger becomes contextWindow × triggerRatio",
			"triggerRatio":  "fraction of contextWindow that trips compaction, default 0.7 — leaving room for the reply plus the next tool result",
			"charsPerToken": "calibrates the fallback estimate used before the provider reports usage; default 4 (English prose), lower it for CJK",
		},
		BestPractices: []string{
			"A decorator, not a strategy: it decides when, the services dep decides how.",
			"The estimate is max(character estimate, reported usage), so it errs toward compacting early.",
		},
	})
}
