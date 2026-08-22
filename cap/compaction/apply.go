package compaction

import (
	"context"

	"github.com/lengzhao/agentkit"
)

// ApplyAll runs compaction services in order, returning the latest message view
// and how many services reported Applied.
func ApplyAll(ctx context.Context, services []Service, req Request) ([]agentkit.ModelMessage, int, error) {
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
