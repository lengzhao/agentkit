package compaction

import "github.com/lengzhao/agentkit"

// IndexedMessage is a model-visible message with its primary source event seq.
type IndexedMessage struct {
	Message     agentkit.ModelMessage
	Seq         agentkit.EventSeq
	IsTurnStart bool
	// LogicalChars is the message's ingest-time logical size recorded in event
	// metadata (pre-sanitize: attachments stripped for storage still count).
	// Zero means unknown; estimators fall back to measuring the stored message.
	// Cut-point math must use it: the stored form can be orders of magnitude
	// smaller than what hydration later puts on the wire.
	LogicalChars int
}

// CutPointResult is the compaction cut point in an indexed message list.
type CutPointResult struct {
	FirstKeptIndex int
	TurnStartIndex int
	IsSplitTurn    bool
}
