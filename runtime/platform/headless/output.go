// Package headless holds the platforms that run without an interactive
// terminal: platform/worker for one-shot batches and platform/timer for
// in-process schedules. Neither ever reads stdin, which is what makes them safe
// under systemd, cron, containers, and anywhere stdin is /dev/null.
package headless

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

	"github.com/lengzhao/agentkit"
)

// Output modes.
const (
	OutputText = "text"
	OutputJSON = "json"
)

// emitter renders outbound events for an unattended run. Results go to stdout so
// they can be piped; progress and diagnostics go to stderr so they cannot
// corrupt that stream.
//
// Writes are serialized because Runner may emit from several turns at once, and a
// JSON line longer than the pipe buffer would otherwise interleave with another
// goroutine's, breaking the one-object-per-line guarantee.
type emitter struct {
	mode   string
	stream bool
	mu     sync.Mutex
	out    io.Writer
	errOut io.Writer
}

func newEmitter(mode string, stream bool) *emitter {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case OutputJSON:
		mode = OutputJSON
	default:
		mode = OutputText
	}
	return &emitter{mode: mode, stream: stream, out: os.Stdout, errOut: os.Stderr}
}

func (e *emitter) send(event agentkit.OutboundEvent) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.mode == OutputJSON {
		return e.sendJSON(event)
	}
	return e.sendText(event)
}

// sendJSON writes one JSON object per event so a caller can consume the run with
// a line-oriented reader.
func (e *emitter) sendJSON(event agentkit.OutboundEvent) error {
	if event.Type == agentkit.EventMessageUpdate && !e.stream {
		return nil
	}
	payload := struct {
		Type      agentkit.EventType `json:"type"`
		SessionID agentkit.SessionID `json:"sessionId"`
		AgentID   agentkit.AgentID   `json:"agentId,omitempty"`
		Data      json.RawMessage    `json:"data,omitempty"`
	}{
		Type:      event.Type,
		SessionID: event.SessionID,
		AgentID:   event.AgentID,
		Data:      event.Data,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(e.out, string(raw))
	return err
}

func (e *emitter) sendText(event agentkit.OutboundEvent) error {
	switch event.Type {
	case agentkit.EventMessageStart:
		return nil
	case agentkit.EventMessageEnd:
		// The finalized message is the result an unattended run exists to
		// produce. When streaming, the text already went out delta by delta, so
		// only terminate the line.
		var payload agentkit.MessageEndPayload
		if err := json.Unmarshal(event.Data, &payload); err != nil {
			return err
		}
		if e.stream {
			_, err := fmt.Fprintln(e.out)
			return err
		}
		if text := textOf(payload.Message.Content); text != "" {
			_, err := fmt.Fprintln(e.out, text)
			return err
		}
		return nil
	case agentkit.EventMessageUpdate:
		if !e.stream {
			return nil
		}
		var payload agentkit.MessageUpdatePayload
		if err := json.Unmarshal(event.Data, &payload); err != nil {
			return err
		}
		switch payload.AssistantMessageEvent.Type {
		case agentkit.AssistantEventTextDelta, agentkit.AssistantEventThinkingDelta:
			_, err := fmt.Fprint(e.out, payload.AssistantMessageEvent.Delta)
			return err
		}
		return nil
	case agentkit.EventAssistantMessage:
		// Emitted by platforms that skip streaming; message/end already covers
		// the streaming path, so avoid printing the same text twice.
		var msg agentkit.ModelMessage
		if err := json.Unmarshal(event.Data, &msg); err != nil {
			return err
		}
		if text := textOf(msg.Content); text != "" {
			_, err := fmt.Fprintln(e.out, text)
			return err
		}
		return nil
	case agentkit.EventTurnContinue:
		var payload struct {
			Segment int    `json:"segment"`
			Reason  string `json:"reason"`
			Steps   int    `json:"steps"`
		}
		if err := json.Unmarshal(event.Data, &payload); err != nil {
			return err
		}
		_, err := fmt.Fprintf(e.errOut, "[continue #%d after %d step(s): %s]\n",
			payload.Segment, payload.Steps, payload.Reason)
		return err
	case agentkit.EventSessionRecovery:
		_, err := fmt.Fprintf(e.errOut, "[recovered interrupted turn: %s]\n", string(event.Data))
		return err
	case "error":
		var payload struct {
			Error string `json:"error"`
		}
		if err := json.Unmarshal(event.Data, &payload); err == nil && payload.Error != "" {
			_, err := fmt.Fprintf(e.errOut, "error: %s\n", payload.Error)
			return err
		}
		_, err := fmt.Fprintf(e.errOut, "error: %s\n", string(event.Data))
		return err
	default:
		return nil
	}
}

func textOf(parts []agentkit.ContentPart) string {
	var b strings.Builder
	for _, part := range parts {
		if part.Type == "text" {
			b.WriteString(part.Text)
		}
	}
	return b.String()
}

func userMessage(text string) agentkit.ModelMessage {
	return agentkit.ModelMessage{
		Role:    "user",
		Content: []agentkit.ContentPart{{Type: "text", Text: text}},
	}
}
