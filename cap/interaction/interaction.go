// Package interaction is the capability boundary for human-in-the-loop prompts
// routed through the inbound platform (CLI stdin, IM cards, etc.).
package interaction

import "context"

// Kind classifies a human-in-the-loop prompt.
type Kind string

const (
	Question     Kind = "question"
	Approval     Kind = "approval"
	Confirmation Kind = "confirmation"
	Choice       Kind = "choice"
)

// Option is one selectable answer.
type Option struct {
	Label string `json:"label"`
}

// Human is a mid-turn prompt routed through the inbound platform.
type Human struct {
	ID       string   `json:"id,omitempty"`
	Kind     Kind     `json:"kind"`
	Prompt   string   `json:"prompt"`
	Options  []Option `json:"options,omitempty"`
	Default  string   `json:"default,omitempty"`
	Multiple bool     `json:"multiple,omitempty"`
}

// Reply is the raw human response before option matching.
type Reply struct {
	Text string `json:"text"`
}

// Result is the resolved outcome of Session.Run.
type Result struct {
	Answered bool   `json:"answered"`
	Text     string `json:"text,omitempty"`
	Selected int    `json:"selected"`
	Reason   string `json:"reason,omitempty"`
}

// Session runs a blocking human-in-the-loop prompt on the session that owns
// the current turn. Loop attaches this via agentkit.KeySessionControl.
type Session interface {
	Run(context.Context, Human) (Result, error)
}

// Handler is implemented by interactive platforms (CLI) that read replies
// synchronously during a turn. Async IM platforms omit this and rely on
// DeliverInteractionReply when inbound messages arrive.
type Handler interface {
	ReadReply(context.Context, Human) (Reply, error)
}

// AsyncPlatform marks IM transports that collect replies through inbound
// messages rather than a synchronous Handler.
type AsyncPlatform interface {
	AsyncInteraction() bool
}

// StartPayload is emitted on interaction/start.
type StartPayload struct {
	ID       string   `json:"id"`
	Kind     Kind     `json:"kind"`
	Prompt   string   `json:"prompt"`
	Options  []Option `json:"options,omitempty"`
	Default  string   `json:"default,omitempty"`
	Multiple bool     `json:"multiple,omitempty"`
}

// EndPayload closes an interaction/start.
type EndPayload struct {
	ID       string `json:"id"`
	Answered bool   `json:"answered"`
	Text     string `json:"text,omitempty"`
	Selected int    `json:"selected"`
	Reason   string `json:"reason,omitempty"`
}

// MatchOption resolves typed answers against Options.
func MatchOption(text string, options []Option) Result {
	if len(options) == 0 {
		return Result{Answered: true, Text: text, Selected: -1}
	}
	labels := make([]string, len(options))
	for i, opt := range options {
		labels[i] = opt.Label
	}
	got := matchLabels(text, labels)
	return Result{
		Answered: got.answered,
		Text:     got.text,
		Selected: got.selected,
	}
}
