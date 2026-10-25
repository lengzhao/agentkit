package acpremote

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	acp "github.com/coder/acp-go-sdk"
	"github.com/lengzhao/agentkit"
)

// steeringControl is the queue owned by Loop for the active session.
type steeringControl interface {
	PopSteering() []agentkit.ModelMessage
	RestoreSteering([]agentkit.ModelMessage)
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
		messages := ctrl.PopSteering()
		for i, msg := range messages {
			select {
			case <-stop:
				ctrl.RestoreSteering(messages[i:])
				return nil
			default:
			}
			requestCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
			raw, err := callSteering(requestCtx, conn, sessionID, msg, abort)
			cancel()
			if err != nil {
				var rpcErr *acp.RequestError
				if errors.As(err, &rpcErr) && rpcErr.Code == -32601 {
					ctrl.RestoreSteering(messages[i:])
					return nil
				}
				// Delivery is ambiguous. Never automatically retry the current message;
				// preserve the rest, and surface the failure to the caller.
				ctrl.RestoreSteering(messages[i+1:])
				return wrapACPStageError("_session/steering", err)
			}
			var response struct {
				Outcome string `json:"outcome"`
			}
			if err = json.Unmarshal(raw, &response); err != nil {
				ctrl.RestoreSteering(messages[i+1:])
				return fmt.Errorf("decode ACP steering response: %w", err)
			}
			switch response.Outcome {
			case "injected":
				if err := accepted(msg); err != nil {
					ctrl.RestoreSteering(messages[i+1:])
					return err
				}
			case "promptRequired":
				ctrl.RestoreSteering(messages[i:])
				return nil
			default:
				ctrl.RestoreSteering(messages[i+1:])
				return fmt.Errorf("unexpected ACP steering outcome %q", response.Outcome)
			}
		}
	}
}

// CallExtension writes synchronously before waiting for the JSON-RPC response.
// A cancelled response wait returns through the SDK without closing the shared
// transport. Only abort if the call stays stuck after cancellation (for example
// in a synchronous pipe write), allowing the SDK a short grace period to return.
func callSteering(ctx context.Context, conn *acp.ClientSideConnection, sessionID acp.SessionId, msg agentkit.ModelMessage, abort func()) (json.RawMessage, error) {
	aborted := make(chan struct{})
	finished := make(chan struct{})
	stopAbort := context.AfterFunc(ctx, func() {
		defer close(aborted)
		timer := time.NewTimer(100 * time.Millisecond)
		defer timer.Stop()
		select {
		case <-finished:
			return
		case <-timer.C:
		}
		select {
		case <-finished:
			return
		default:
			abort()
		}
	})
	raw, err := conn.CallExtension(ctx, "_session/steering", acp.PromptRequest{
		SessionId: sessionID, Prompt: modelMessageToPrompt(msg),
		Meta: map[string]any{"steering": map[string]any{"idleBehavior": "promptRequired"}},
	})
	close(finished)
	if !stopAbort() {
		<-aborted
	}
	return raw, err
}
