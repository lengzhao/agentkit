package compaction

import (
	"context"
	"fmt"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/telemetry"
)

// ApplyAll runs compaction services in order, returning the latest message view
// and how many services reported Applied.
func ApplyAll(ctx context.Context, services []Service, req Request) ([]agentkit.ModelMessage, int, error) {
	messages, applied, err := applyAll(ctx, services, req)
	if len(services) == 0 || (applied == 0 && err == nil) {
		return messages, applied, err
	}
	mode := "automatic"
	if req.Force {
		mode = "force"
	}
	_, endObservation := telemetry.BeginObservation(ctx, telemetry.ObservationMeta{
		Name:  "compaction.apply",
		Kind:  telemetry.KindSpan,
		Input: mode,
	})
	var observationEnd telemetry.ObservationEnd
	if err != nil {
		observationEnd.Err = err
	} else {
		observationEnd.Output = fmt.Sprintf("applied %d service(s)", applied)
	}
	endObservation(observationEnd)
	return messages, applied, err
}

func applyAll(ctx context.Context, services []Service, req Request) ([]agentkit.ModelMessage, int, error) {
	messages := req.Messages
	applied := 0
	for _, svc := range services {
		if svc == nil {
			continue
		}
		call := req
		call.Messages = messages
		result, err := svc.Compact(ctx, call)
		if err != nil {
			return messages, applied, err
		}
		if len(result.Messages) > 0 {
			messages = result.Messages
		}
		if result.Applied {
			applied++
			if len(result.Messages) == 0 && req.Session != nil {
				derived, err := req.Session.DeriveMessages(ctx)
				if err != nil {
					return messages, applied, err
				}
				messages = derived
			}
		}
	}
	return messages, applied, nil
}
