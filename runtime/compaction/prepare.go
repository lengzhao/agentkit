package compaction

import (
	capscompaction "github.com/lengzhao/agentkit/cap/compaction"
	"github.com/lengzhao/agentkit"
)

// Prepare builds a compaction plan from indexed messages. boundaryStart is the
// first index eligible for summarization (after any previous compaction summary).
func Prepare(indexed []capscompaction.IndexedMessage, boundaryStart int, keepRecentTokens int, previousSummary string, tokensBefore int) *capscompaction.Preparation {
	if boundaryStart < 0 {
		boundaryStart = 0
	}
	if boundaryStart > len(indexed) {
		return nil
	}
	if tokensBefore <= 0 {
		tokensBefore = estimateIndexedTokens(indexed)
	}

	cut := FindCutPoint(indexed, boundaryStart, len(indexed), keepRecentTokens)
	historyEnd := cut.FirstKeptIndex
	if cut.IsSplitTurn {
		historyEnd = cut.TurnStartIndex
	}

	toSummarize := indexedMessages(indexed[boundaryStart:historyEnd])
	turnPrefix := []agentkit.ModelMessage{}
	if cut.IsSplitTurn {
		turnPrefix = indexedMessages(indexed[cut.TurnStartIndex:cut.FirstKeptIndex])
	}
	retained := indexedMessages(indexed[cut.FirstKeptIndex:])

	if len(toSummarize) == 0 && len(turnPrefix) == 0 {
		return nil
	}
	if len(retained) == 0 {
		return nil
	}

	firstKeptSeq := indexed[cut.FirstKeptIndex].Seq
	return &capscompaction.Preparation{
		FirstKeptSeq:        firstKeptSeq,
		FirstKeptIndex:      cut.FirstKeptIndex,
		MessagesToSummarize: toSummarize,
		TurnPrefixMessages:  turnPrefix,
		RetainedTail:        retained,
		PreviousSummary:     previousSummary,
		TokensBefore:        tokensBefore,
		IsSplitTurn:         cut.IsSplitTurn,
	}
}

func indexedMessages(indexed []capscompaction.IndexedMessage) []agentkit.ModelMessage {
	out := make([]agentkit.ModelMessage, len(indexed))
	for i, item := range indexed {
		out[i] = item.Message
	}
	return out
}

func estimateIndexedTokens(indexed []capscompaction.IndexedMessage) int {
	total := 0
	for _, item := range indexed {
		total += EstimateTokens(item.Message)
	}
	return total
}
