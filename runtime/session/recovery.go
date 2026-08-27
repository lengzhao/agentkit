package session

import (
	"context"
	"encoding/json"

	"github.com/lengzhao/agentkit"
)

// InterruptedToolResultText is the tool result synthesized for a call the
// process never got to answer. It is model-visible on purpose: the agent needs
// to know the call was cut off rather than silently succeeded.
const InterruptedToolResultText = "tool execution was interrupted before it produced a result (process exited); re-run it if the result is still needed"

// interruptedDecision marks synthesized results in the tool audit trail.
const interruptedDecision = "interrupted"

// IncompleteTurn describes a turn that never reached turn/end, which is what a
// crash, SIGKILL, or power loss leaves behind.
type IncompleteTurn struct {
	TurnStartSeq agentkit.EventSeq
	AgentID      agentkit.AgentID
	StepsStarted int
	StepsEnded   int
	// OpenStep is the index of a step/start with no step/end, or -1 when the
	// last step closed cleanly.
	OpenStep int
	// OrphanCalls are tool calls in the derived history that no tool result
	// answers. Left alone they make the history unusable: providers reject an
	// assistant message whose tool calls have no replies.
	OrphanCalls []agentkit.ToolCall
}

// RecoveryData is the audit payload of a session/recovery event.
type RecoveryData struct {
	TurnStartSeq  agentkit.EventSeq `json:"turnStartSeq"`
	Steps         int               `json:"steps"`
	OrphanResults int               `json:"orphanResults"`
	ClosedStep    int               `json:"closedStep"`
	Reason        string            `json:"reason"`
}

// ScanIncomplete reports the trailing unterminated turn, or nil when the log
// ends cleanly. Only the last turn can be incomplete: every earlier turn either
// wrote turn/end or was repaired on a previous startup.
func ScanIncomplete(events []agentkit.SessionEvent) *IncompleteTurn {
	var open *IncompleteTurn
	for _, ev := range events {
		switch ev.Type {
		case agentkit.EventTurnStart:
			open = &IncompleteTurn{
				TurnStartSeq: ev.Seq,
				AgentID:      ev.AgentID,
				OpenStep:     -1,
			}
		case agentkit.EventTurnEnd:
			open = nil
		case agentkit.EventStepStart:
			if open == nil {
				continue
			}
			open.StepsStarted++
			var data StepStartData
			if err := json.Unmarshal(ev.Data, &data); err == nil {
				open.OpenStep = data.Step
			}
		case agentkit.EventStepEnd:
			if open == nil {
				continue
			}
			open.StepsEnded++
			open.OpenStep = -1
		}
	}
	if open == nil {
		return nil
	}
	open.OrphanCalls = orphanToolCalls(events, open.TurnStartSeq)
	return open
}

// orphanToolCalls pairs assistant tool calls against tool results after seq.
// Pairing is by call ID and consumes each result once, because scripted and
// retried runs can reuse the same ID across steps.
func orphanToolCalls(events []agentkit.SessionEvent, seq agentkit.EventSeq) []agentkit.ToolCall {
	answered := make(map[agentkit.ToolCallID]int)
	for _, ev := range events {
		if ev.Type != agentkit.EventToolResult || ev.Seq <= seq {
			continue
		}
		var result agentkit.ToolResult
		if err := json.Unmarshal(ev.Data, &result); err != nil {
			continue
		}
		answered[result.ID]++
	}

	var orphans []agentkit.ToolCall
	for _, ev := range events {
		if ev.Type != agentkit.EventAssistantMessage || ev.Seq <= seq {
			continue
		}
		var msg agentkit.ModelMessage
		if err := json.Unmarshal(ev.Data, &msg); err != nil {
			continue
		}
		for _, call := range msg.ToolCalls {
			if answered[call.ID] > 0 {
				answered[call.ID]--
				continue
			}
			orphans = append(orphans, call)
		}
	}
	return orphans
}

// RepairIncomplete makes an interrupted turn replayable and closes it: it
// answers every orphan tool call, ends the open step, writes turn/end, then
// records a session/recovery event for audit.
func RepairIncomplete(ctx context.Context, s agentkit.Session, turn *IncompleteTurn) (RecoveryData, error) {
	if turn == nil {
		return RecoveryData{}, nil
	}
	agentID := turn.AgentID
	for _, call := range turn.OrphanCalls {
		if err := AppendToolResult(ctx, s, agentID, InterruptedToolResult(call)); err != nil {
			return RecoveryData{}, err
		}
	}
	if turn.OpenStep >= 0 {
		if err := AppendStepEnd(ctx, s, agentID, turn.OpenStep); err != nil {
			return RecoveryData{}, err
		}
	}
	if err := AppendTurnEnd(ctx, s, agentID, turn.StepsEnded); err != nil {
		return RecoveryData{}, err
	}
	data := RecoveryData{
		TurnStartSeq:  turn.TurnStartSeq,
		Steps:         turn.StepsEnded,
		OrphanResults: len(turn.OrphanCalls),
		ClosedStep:    turn.OpenStep,
		Reason:        "turn/start without turn/end",
	}
	if err := AppendSessionRecovery(ctx, s, agentID, data); err != nil {
		return RecoveryData{}, err
	}
	return data, nil
}

// InterruptedToolResult is the stand-in result for a call that never ran to
// completion.
func InterruptedToolResult(call agentkit.ToolCall) agentkit.ToolResult {
	return agentkit.ToolResult{
		ID:      call.ID,
		Name:    call.Name,
		Content: InterruptedToolResultText,
		Audit:   map[string]string{"decision": interruptedDecision},
	}
}

func AppendSessionRecovery(ctx context.Context, s agentkit.Session, agentID agentkit.AgentID, data RecoveryData) error {
	return appendLifecycle(ctx, s, agentID, agentkit.EventSessionRecovery, data)
}
