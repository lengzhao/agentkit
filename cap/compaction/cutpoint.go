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

func isCutPointMessage(msg agentkit.ModelMessage) bool {
	switch msg.Role {
	case "user", "assistant":
		return true
	default:
		return false
	}
}

func isTurnStartMessage(msg agentkit.ModelMessage) bool {
	return msg.Role == "user"
}

// FindCutPoint walks backward until keepRecentTokens is reached, then cuts at the
// nearest valid cut point. Tool messages are never cut points (Pi-compatible).
func FindCutPoint(messages []IndexedMessage, startIndex, endIndex, keepRecentTokens int) CutPointResult {
	if startIndex < 0 {
		startIndex = 0
	}
	if endIndex > len(messages) {
		endIndex = len(messages)
	}
	if startIndex >= endIndex || keepRecentTokens <= 0 {
		return CutPointResult{FirstKeptIndex: startIndex, TurnStartIndex: -1}
	}

	cutPoints := make([]int, 0, endIndex-startIndex)
	for i := startIndex; i < endIndex; i++ {
		if isCutPointMessage(messages[i].Message) {
			cutPoints = append(cutPoints, i)
		}
	}
	if len(cutPoints) == 0 {
		return CutPointResult{FirstKeptIndex: startIndex, TurnStartIndex: -1}
	}

	accumulated := 0
	cutIndex := cutPoints[0]
	for i := endIndex - 1; i >= startIndex; i-- {
		tokens := EstimateTokens(messages[i].Message)
		if tokens == 0 {
			continue
		}
		accumulated += tokens
		if accumulated >= keepRecentTokens {
			for _, c := range cutPoints {
				if c >= i {
					cutIndex = c
					break
				}
			}
			break
		}
	}

	startsTurn := messages[cutIndex].IsTurnStart
	turnStartIndex := -1
	if !startsTurn {
		for i := cutIndex; i >= startIndex; i-- {
			if messages[i].IsTurnStart {
				turnStartIndex = i
				break
			}
		}
	}

	return CutPointResult{
		FirstKeptIndex: cutIndex,
		TurnStartIndex: turnStartIndex,
		IsSplitTurn:    !startsTurn && turnStartIndex >= 0,
	}
}
