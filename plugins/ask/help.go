package ask

import "github.com/lengzhao/agentkit/plugindoc"

func init() {
	plugindoc.Register("ask/cli", plugindoc.Doc{
		Summary: "Ask the human a question on the terminal and read the answer from stdin.",
		ConfigNotes: map[string]string{
			"prefix": `label printed before the question; defaults to "[agent asks]"`,
		},
		BestPractices: []string{
			"Only for interactive platforms; use ask/unavailable for worker / timer / cron runs.",
			"A closed stdin returns an unanswered result rather than an error, so a cron run degrades instead of hanging.",
			"The answer is read in a goroutine so tool timeouts and turn cancellation still apply.",
			"Questions are serialised: two concurrent sessions cannot interleave prompts on one terminal.",
			"A line typed after its question timed out is discarded, not adopted by the next question.",
		},
	})
	plugindoc.Register("ask/unavailable", plugindoc.Doc{
		Summary: "Answer every question with \"nobody is available\", for unattended runs.",
		ConfigNotes: map[string]string{
			"reason": "text handed to the model; say what it should do instead",
		},
		BestPractices: []string{
			"Wire this wherever nobody is watching, so tool/ask-user degrades instead of blocking.",
		},
	})
}
