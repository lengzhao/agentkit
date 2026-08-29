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

// Prepare builds a compaction plan from indexed messages. boundaryStart is the
// first index eligible for summarization (after any previous compaction summary).
func Prepare(indexed []IndexedMessage, boundaryStart int, keepRecentTokens int, previousSummary string, tokensBefore int) *Preparation {
	if boundaryStart < 0 {
		boundaryStart = 0
	}
	if boundaryStart > len(indexed) {
		return nil
	}
	if tokensBefore <= 0 {
		tokensBefore = EstimateIndexedTokens(indexed)
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
	return &Preparation{
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

func indexedMessages(indexed []IndexedMessage) []agentkit.ModelMessage {
	out := make([]agentkit.ModelMessage, len(indexed))
	for i, item := range indexed {
		out[i] = item.Message
	}
	return out
}

func EstimateIndexedTokens(indexed []IndexedMessage) int {
	total := 0
	for _, item := range indexed {
		total += EstimateTokens(item.Message)
	}
	return total
}

// MemoryCutoffSeq returns the highest event seq dropped from hot memory.
func (d EventData) MemoryCutoffSeq() agentkit.EventSeq {
	if d.FirstKeptSeq > 0 {
		if d.FirstKeptSeq > 1 {
			return d.FirstKeptSeq - 1
		}
		return 0
	}
	return d.BeforeSeq
}

// PreviousSummaryText extracts prior summary body from a compaction event.
func PreviousSummaryText(data EventData) string {
	for _, part := range data.Summary.Content {
		if part.Type == "text" {
			return part.Text
		}
	}
	return ""
}
