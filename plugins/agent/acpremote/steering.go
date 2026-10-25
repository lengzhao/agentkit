package acpremote

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	acp "github.com/coder/acp-go-sdk"
	"github.com/lengzhao/agentkit"
	capsession "github.com/lengzhao/agentkit/cap/session"
)

// steeringControl is the queue owned by Loop for the active session.
type steeringControl interface {
	PopSteering() []agentkit.SteerEntry
	RestoreSteering([]agentkit.SteerEntry)
}

func supportsSteering(meta map[string]any) bool {
	capability, _ := meta["steering"].(map[string]any)
	supported, _ := capability["supported"].(bool)
	return supported
}

// forwardSteering runs alongside the original prompt. Stopping waits for an
// outstanding request to settle: cancelling it at prompt completion could lose
// its acknowledgement and cause a duplicate follow-up. Each request is bounded.
func forwardSteering(ctx context.Context, stop <-chan struct{}, ctrl steeringControl, conn *acp.ClientSideConnection, sessionID acp.SessionId, abort func(), accepted func(agentkit.ModelMessage) error) error {
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-stop:
			return nil
		case <-ticker.C:
		}
		// Prefer leaving queued input to Loop once the original prompt has settled.
		select {
		case <-stop:
			return nil
		default:
		}
		entries := ctrl.PopSteering()
		for i, entry := range entries {
			select {
			case <-stop:
				ctrl.RestoreSteering(entries[i:])
				return nil
			default:
			}
			requestCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			raw, err := callSteering(requestCtx, conn, sessionID, entry.Message, abort)
			cancel()
			if err != nil {
				var rpcErr *acp.RequestError
				if errors.As(err, &rpcErr) && rpcErr.Code == -32601 {
					// Capability was negotiated away mid-turn; leave the queue for
					// Loop's next-turn fallback.
					ctrl.RestoreSteering(entries[i:])
					return nil
				}
				// Delivery is ambiguous. Never automatically retry the current message;
				// preserve the rest, and surface the failure to the caller.
				ackSteer(entry, capsession.SteerOutcomeRejected)
				ctrl.RestoreSteering(entries[i+1:])
				return wrapACPStageError("_session/steering", err)
			}
			var response struct {
				Outcome string `json:"outcome"`
			}
			if err = json.Unmarshal(raw, &response); err != nil {
				ctrl.RestoreSteering(entries[i+1:])
				return fmt.Errorf("decode ACP steering response: %w", err)
			}
			switch response.Outcome {
			case "injected":
				if err := accepted(entry.Message); err != nil {
					ctrl.RestoreSteering(entries[i+1:])
					return err
				}
				// The peer folded the message into the streaming prompt: that is
				// the authoritative mid-turn injection.
				ackSteer(entry, capsession.SteerOutcomeInjected)
			case "promptRequired":
				// The peer was too far along to inject; the message stays queued
				// and Loop runs it as the session's next turn (acked there).
				ctrl.RestoreSteering(entries[i:])
				return nil
			default:
				ctrl.RestoreSteering(entries[i+1:])
				return fmt.Errorf("unexpected ACP steering outcome %q", response.Outcome)
			}
		}
	}
}

// ackSteer reports one steered message's terminal outcome. Best effort: the ack
// runs platform I/O, so it must not break the steering worker.
func ackSteer(entry agentkit.SteerEntry, outcome string) {
	if entry.Ack != nil {
		entry.Ack(outcome)
	}
}

// CallExtension writes synchronously before waiting for the JSON-RPC response.
// Context cancellation alone cannot interrupt a blocked pipe write, so abort
// must close the transport (in production, terminate the owning subprocess).
func callSteering(ctx context.Context, conn *acp.ClientSideConnection, sessionID acp.SessionId, msg agentkit.ModelMessage, abort func()) (json.RawMessage, error) {
	aborted := make(chan struct{})
	stopAbort := context.AfterFunc(ctx, func() { defer close(aborted); abort() })
	raw, err := conn.CallExtension(ctx, "_session/steering", acp.PromptRequest{
		SessionId: sessionID, Prompt: modelMessageToPrompt(msg),
		Meta: map[string]any{"steering": map[string]any{"idleBehavior": "promptRequired"}},
	})
	if !stopAbort() {
		<-aborted
	}
	return raw, err
}
