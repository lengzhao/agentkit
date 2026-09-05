package compaction

import "github.com/lengzhao/agentkit"

// Preparation is the Pi-style compaction plan for one pass.
type Preparation struct {
	FirstKeptSeq        agentkit.EventSeq
	FirstKeptIndex      int
	MessagesToSummarize []agentkit.ModelMessage
	TurnPrefixMessages  []agentkit.ModelMessage
	RetainedTail        []agentkit.ModelMessage
	PreviousSummary     string
	TokensBefore        int
	IsSplitTurn         bool
}
