package telemetry

import (
	"context"
	"strings"

	"github.com/lengzhao/agentkit"
)

type turnAccumKey struct{}

type turnAccum struct {
	parts    []string
	usage    Usage
	hasUsage bool
}

// WithTurnAccum stores per-turn output and usage accumulators on ctx.
func WithTurnAccum(ctx context.Context) context.Context {
	if turnAccumFrom(ctx) != nil {
		return ctx
	}
	return context.WithValue(ctx, turnAccumKey{}, &turnAccum{})
}

func turnAccumFrom(ctx context.Context) *turnAccum {
	acc, _ := ctx.Value(turnAccumKey{}).(*turnAccum)
	return acc
}

// RecordTurnOutput appends one final assistant reply to the turn trace output.
func RecordTurnOutput(ctx context.Context, msg agentkit.ModelMessage) {
	if text := userVisibleText(msg); text != "" {
		appendTurnUserVisible(ctx, text)
	}
}

func appendTurnUserVisible(ctx context.Context, text string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	acc := turnAccumFrom(ctx)
	if acc == nil {
		return
	}
	acc.parts = append(acc.parts, text)
}

// RecordTurnUsage adds one step's token usage to the active turn total.
func RecordTurnUsage(ctx context.Context, usage Usage) {
	if usage.InputTokens == 0 && usage.OutputTokens == 0 && usage.TotalTokens == 0 {
		return
	}
	acc := turnAccumFrom(ctx)
	if acc == nil {
		return
	}
	acc.usage.InputTokens += usage.InputTokens
	acc.usage.OutputTokens += usage.OutputTokens
	total := usage.TotalTokens
	if total == 0 {
		total = usage.InputTokens + usage.OutputTokens
	}
	acc.usage.TotalTokens += total
	acc.hasUsage = true
}

// TurnEndFromAccum reads accumulated turn output and usage for trace closure.
func TurnEndFromAccum(ctx context.Context) TurnEnd {
	acc := turnAccumFrom(ctx)
	if acc == nil {
		return TurnEnd{}
	}
	end := TurnEnd{}
	if len(acc.parts) > 0 {
		end.Output = strings.Join(acc.parts, turnUserVisibleSep)
	}
	if acc.hasUsage {
		usage := acc.usage
		end.Usage = &usage
	}
	return end
}
