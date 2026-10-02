package compaction

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/lengzhao/agentkit"
	capscompaction "github.com/lengzhao/agentkit/cap/compaction"
	captelemetry "github.com/lengzhao/agentkit/cap/telemetry"
	"github.com/lengzhao/agentkit/runtime/telemetry"
)

type applySpanContextKey struct{}

// ApplyAll runs compaction services in order with compaction telemetry.
func ApplyAll(ctx context.Context, services []capscompaction.Service, req capscompaction.Request) ([]agentkit.ModelMessage, int, error) {
	outermost := ctx.Value(applySpanContextKey{}) == nil
	if outermost {
		ctx = context.WithValue(ctx, applySpanContextKey{}, true)
	}

	tokensBefore := EstimateMessagesTokens(req.Messages)
	charsBefore := estimateMessagesChars(req.Messages)
	messagesBefore := len(req.Messages)
	seqBefore := latestCompactionSeq(ctx, req.Session)
	started := time.Now()

	messages, applied, err := applyAll(ctx, services, req)
	if !outermost || len(services) == 0 || (applied == 0 && err == nil) {
		return messages, applied, err
	}
	// Only persisted compaction counts as real: services like prune-tool-results
	// report Applied for in-memory trimming that the next step re-reads in full,
	// which would otherwise emit a span on every step without shrinking anything.
	if err == nil && req.Session != nil && latestCompactionSeq(ctx, req.Session) <= seqBefore {
		return messages, applied, err
	}

	mode := "automatic"
	if req.Force {
		mode = "force"
	}
	tokensAfter := EstimateMessagesTokens(messages)
	charsAfter := estimateMessagesChars(messages)
	durationMs := time.Since(started).Milliseconds()
	_, endObservation := telemetry.BeginObservation(ctx, telemetry.ObservationMetaFromContext(ctx, captelemetry.ObservationMeta{
		Name:      "compaction.apply",
		Kind:      captelemetry.KindSpan,
		Input:     mode,
		AgentID:   string(req.AgentID),
		SessionID: string(req.SessionID),
		Attributes: map[string]string{
			"mode":            mode,
			"applied":         strconv.Itoa(applied),
			"tokens_before":   strconv.Itoa(tokensBefore),
			"tokens_after":    strconv.Itoa(tokensAfter),
			"chars_before":    strconv.Itoa(charsBefore),
			"chars_after":     strconv.Itoa(charsAfter),
			"messages_before": strconv.Itoa(messagesBefore),
			"messages_after":  strconv.Itoa(len(messages)),
			"duration_ms":     strconv.FormatInt(durationMs, 10),
			"session_id":      string(req.SessionID),
		},
	}))
	observationEnd := captelemetry.ObservationEnd{
		Output: fmt.Sprintf(
			"applied=%d tokens_before=%d tokens_after=%d chars_before=%d chars_after=%d messages_before=%d messages_after=%d duration_ms=%d",
			applied, tokensBefore, tokensAfter, charsBefore, charsAfter, messagesBefore, len(messages), durationMs,
		),
	}
	if err != nil {
		observationEnd.Err = err
	}
	endObservation(observationEnd)
	return messages, applied, err
}

// latestCompactionSeq returns the newest session/compaction event seq, or 0.
// A nil session (or read failure) yields 0.
func latestCompactionSeq(ctx context.Context, sess agentkit.Session) agentkit.EventSeq {
	if sess == nil {
		return 0
	}
	events, err := sess.Read(ctx, 0)
	if err != nil {
		return 0
	}
	var seq agentkit.EventSeq
	for _, ev := range events {
		if ev.Type == agentkit.EventCompaction && ev.Seq > seq {
			seq = ev.Seq
		}
	}
	return seq
}

func estimateMessagesChars(messages []agentkit.ModelMessage) int {
	total := 0
	for _, msg := range messages {
		total += estimateMessageChars(msg)
	}
	return total
}
