package compaction

import "github.com/lengzhao/agentkit"

// IndexedMessage is a model-visible message with its primary source event seq.
type IndexedMessage struct {
	Message     agentkit.ModelMessage
	Seq         agentkit.EventSeq
	IsTurnStart bool
}

// CutPointResult is the compaction cut point in an indexed message list.
type CutPointResult struct {
	FirstKeptIndex int
	TurnStartIndex int
	IsSplitTurn    bool
}
