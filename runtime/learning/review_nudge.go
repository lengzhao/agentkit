package learning

import (
	"strings"

	"github.com/lengzhao/agentkit"
	caplearning "github.com/lengzhao/agentkit/cap/learning"
	rtsession "github.com/lengzhao/agentkit/runtime/session"
)

// DefaultMemoryNudgeInterval is Hermes memory.nudge_interval (user turns between background memory reviews).
const DefaultMemoryNudgeInterval = 10

// NudgeSessionState tracks background memory review cadence for one conversation.
type NudgeSessionState = caplearning.NudgeSessionState

// NudgeFile maps session IDs to nudge counters (per tenant workspace file).
type NudgeFile struct {
	Sessions map[string]NudgeSessionState `json:"sessions"`
}

// CountUserTurns counts user-role messages with non-empty text (slash commands still count as turns).
func CountUserTurns(messages []agentkit.ModelMessage) int {
	n := 0
	for _, msg := range messages {
		if msg.Role != "user" {
			continue
		}
		if strings.TrimSpace(rtsession.FlattenTextParts(msg.Content, "\n")) == "" {
			continue
		}
		n++
	}
	return n
}

// TurnSegmentUsedMemoryTool reports whether the current turn (since the last user message) invoked memory.
func TurnSegmentUsedMemoryTool(messages []agentkit.ModelMessage) bool {
	lastUser := -1
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "user" {
			lastUser = i
			break
		}
	}
	if lastUser < 0 {
		return false
	}
	for i := lastUser; i < len(messages); i++ {
		msg := messages[i]
		for _, call := range msg.ToolCalls {
			if strings.TrimSpace(call.Name) == "memory" {
				return true
			}
		}
		for _, res := range msg.ToolResults {
			if strings.TrimSpace(res.Name) == "memory" {
				return true
			}
		}
	}
	return false
}

// MemoryNudgeDecision is whether to spawn background memory review after this turn.
type MemoryNudgeDecision struct {
	RunReview        bool
	TurnsSinceMemory int
	Reason           string
}

// DecideMemoryNudge applies Hermes-style cadence: increment per completed user turn; reset when memory
// tool ran; run review when turnsSince >= interval. interval <= 0 disables automatic memory review.
func DecideMemoryNudge(
	interval int,
	messages []agentkit.ModelMessage,
	prior NudgeSessionState,
	hydrateFromHistory bool,
) (MemoryNudgeDecision, NudgeSessionState) {
	next := prior
	if interval <= 0 {
		return MemoryNudgeDecision{RunReview: false, TurnsSinceMemory: next.TurnsSinceMemory, Reason: "nudge disabled"}, next
	}
	if TurnSegmentUsedMemoryTool(messages) {
		next.TurnsSinceMemory = 0
		return MemoryNudgeDecision{RunReview: false, TurnsSinceMemory: 0, Reason: "memory tool used this turn"}, next
	}
	if hydrateFromHistory && next.TurnsSinceMemory == 0 {
		priorTurns := CountUserTurns(messages)
		if priorTurns > 1 {
			// Completed user turns before this one; modulo leaves room to increment below.
			next.TurnsSinceMemory = (priorTurns - 1) % interval
		}
	}
	next.TurnsSinceMemory++
	if next.TurnsSinceMemory < interval {
		return MemoryNudgeDecision{
			RunReview:        false,
			TurnsSinceMemory: next.TurnsSinceMemory,
			Reason:           "below nudge interval",
		}, next
	}
	next.TurnsSinceMemory = 0
	return MemoryNudgeDecision{RunReview: true, TurnsSinceMemory: 0, Reason: "nudge interval reached"}, next
}
