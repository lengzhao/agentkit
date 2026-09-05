package compaction

import (
	capscompaction "github.com/lengzhao/agentkit/cap/compaction"
	"github.com/lengzhao/agentkit"
)

func isCutPointMessage(msg agentkit.ModelMessage) bool {
	switch msg.Role {
	case "user", "assistant":
		return true
	default:
		return false
	}
}

// FindCutPoint walks backward until keepRecentTokens is reached, then cuts at the
// nearest valid cut point. Tool messages are never cut points (Pi-compatible).
func FindCutPoint(messages []capscompaction.IndexedMessage, startIndex, endIndex, keepRecentTokens int) capscompaction.CutPointResult {
	if startIndex < 0 {
		startIndex = 0
	}
	if endIndex > len(messages) {
		endIndex = len(messages)
	}
	if startIndex >= endIndex || keepRecentTokens <= 0 {
		return capscompaction.CutPointResult{FirstKeptIndex: startIndex, TurnStartIndex: -1}
	}

	cutPoints := make([]int, 0, endIndex-startIndex)
	for i := startIndex; i < endIndex; i++ {
		if isCutPointMessage(messages[i].Message) {
			cutPoints = append(cutPoints, i)
		}
	}
	if len(cutPoints) == 0 {
		return capscompaction.CutPointResult{FirstKeptIndex: startIndex, TurnStartIndex: -1}
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

	return capscompaction.CutPointResult{
		FirstKeptIndex: cutIndex,
		TurnStartIndex: turnStartIndex,
		IsSplitTurn:    !startsTurn && turnStartIndex >= 0,
	}
}
