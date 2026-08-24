package headless

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/lengzhao/agentkit"
)

func newTestEmitter(mode string, stream bool) (*emitter, *bytes.Buffer, *bytes.Buffer) {
	e := newEmitter(mode, stream)
	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	e.out, e.errOut = out, errOut
	return e, out, errOut
}

func messageEnd(text string) agentkit.OutboundEvent {
	return agentkit.OutboundEvent{
		SessionID: "s:1",
		Type:      agentkit.EventMessageEnd,
		Data: agentkit.MarshalOutboundData(agentkit.MessageEndPayload{
			Message: agentkit.ModelMessage{
				Role:    "assistant",
				Content: []agentkit.ContentPart{{Type: "text", Text: text}},
			},
		}),
	}
}

// TestEmitterPrintsResultWithoutStreaming is the regression guard for a silent
// worker: with streaming off, the finalized message is the only thing that
// carries the result, so suppressing updates must not suppress the answer.
func TestEmitterPrintsResultWithoutStreaming(t *testing.T) {
	t.Parallel()

	e, out, _ := newTestEmitter(OutputText, false)
	if err := e.send(messageEnd("the answer")); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(out.String()); got != "the answer" {
		t.Fatalf("stdout = %q, want %q", got, "the answer")
	}
}

func TestEmitterDoesNotDoublePrintWhenStreaming(t *testing.T) {
	t.Parallel()

	e, out, _ := newTestEmitter(OutputText, true)
	update := agentkit.OutboundEvent{
		Type: agentkit.EventMessageUpdate,
		Data: agentkit.MarshalOutboundData(agentkit.MessageUpdatePayload{
			AssistantMessageEvent: agentkit.AssistantMessageEvent{
				Type:  agentkit.AssistantEventTextDelta,
				Delta: "the answer",
			},
		}),
	}
	if err := e.send(update); err != nil {
		t.Fatal(err)
	}
	if err := e.send(messageEnd("the answer")); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(out.String()); got != "the answer" {
		t.Fatalf("stdout = %q, want the text exactly once", got)
	}
}

func TestEmitterKeepsDiagnosticsOffStdout(t *testing.T) {
	t.Parallel()

	e, out, errOut := newTestEmitter(OutputText, false)
	events := []agentkit.OutboundEvent{
		{Type: agentkit.EventTurnContinue, Data: []byte(`{"segment":1,"reason":"no-tool-calls","steps":3}`)},
		{Type: agentkit.EventSessionRecovery, Data: []byte(`{"orphanResults":1}`)},
		{Type: "error", Data: []byte(`{"error":"boom"}`)},
	}
	for _, event := range events {
		if err := e.send(event); err != nil {
			t.Fatal(err)
		}
	}
	if out.Len() != 0 {
		t.Fatalf("stdout must stay clean for piping, got %q", out.String())
	}
	for _, want := range []string{"continue #1", "recovered", "boom"} {
		if !strings.Contains(errOut.String(), want) {
			t.Fatalf("stderr missing %q:\n%s", want, errOut.String())
		}
	}
}

func TestEmitterJSONModeEmitsOneObjectPerLine(t *testing.T) {
	t.Parallel()

	e, out, errOut := newTestEmitter(OutputJSON, false)
	if err := e.send(messageEnd("done")); err != nil {
		t.Fatal(err)
	}
	if err := e.send(agentkit.OutboundEvent{
		SessionID: "s:1",
		Type:      "error",
		Data:      []byte(`{"error":"boom"}`),
	}); err != nil {
		t.Fatal(err)
	}
	if errOut.Len() != 0 {
		t.Fatalf("json mode should route everything to stdout, stderr = %q", errOut.String())
	}

	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("lines = %d, want 2:\n%s", len(lines), out.String())
	}
	for i, line := range lines {
		var payload struct {
			Type      string          `json:"type"`
			SessionID string          `json:"sessionId"`
			Data      json.RawMessage `json:"data"`
		}
		if err := json.Unmarshal([]byte(line), &payload); err != nil {
			t.Fatalf("line %d is not valid JSON: %v (%s)", i, err, line)
		}
		if payload.SessionID != "s:1" {
			t.Fatalf("line %d sessionId = %q", i, payload.SessionID)
		}
	}
}

func TestEmitterJSONModeSuppressesDeltasUnlessStreaming(t *testing.T) {
	t.Parallel()

	update := agentkit.OutboundEvent{
		Type: agentkit.EventMessageUpdate,
		Data: agentkit.MarshalOutboundData(agentkit.MessageUpdatePayload{
			AssistantMessageEvent: agentkit.AssistantMessageEvent{
				Type:  agentkit.AssistantEventTextDelta,
				Delta: "partial",
			},
		}),
	}

	quiet, quietOut, _ := newTestEmitter(OutputJSON, false)
	if err := quiet.send(update); err != nil {
		t.Fatal(err)
	}
	if quietOut.Len() != 0 {
		t.Fatalf("deltas should be suppressed, got %q", quietOut.String())
	}

	loud, loudOut, _ := newTestEmitter(OutputJSON, true)
	if err := loud.send(update); err != nil {
		t.Fatal(err)
	}
	if loudOut.Len() == 0 {
		t.Fatal("streaming json should emit delta events")
	}
}

func TestEmitterDefaultsToTextForUnknownMode(t *testing.T) {
	t.Parallel()

	e, out, _ := newTestEmitter("yaml-please", false)
	if err := e.send(messageEnd("plain")); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(out.String()); got != "plain" {
		t.Fatalf("stdout = %q, want plain text fallback", got)
	}
}
