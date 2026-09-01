package telemetry

import (
	"context"
	"strings"

	"github.com/lengzhao/agentkit"
)

type turnAccumKey struct{}

type turnAccum struct {
	parts      []string
	usage      Usage
	hasUsage   bool
	steps      int
	stopReason string
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

// RecordTurnSteps records how many model steps completed in the active turn.
func RecordTurnSteps(ctx context.Context, steps int) {
	if steps <= 0 {
		return
	}
	acc := turnAccumFrom(ctx)
	if acc == nil {
		return
	}
	acc.steps = steps
}

// RecordTurnStopReason records why the turn stopped.
func RecordTurnStopReason(ctx context.Context, reason string) {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return
	}
	acc := turnAccumFrom(ctx)
	if acc == nil {
		return
	}
	acc.stopReason = reason
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
	if acc.steps > 0 {
		end.Steps = acc.steps
	}
	end.StopReason = acc.stopReason
	return end
}
