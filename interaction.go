package agentkit

import "context"

// InteractionKind classifies a human-in-the-loop prompt.
type InteractionKind string

const (
	InteractionQuestion     InteractionKind = "question"
	InteractionApproval     InteractionKind = "approval"
	InteractionConfirmation InteractionKind = "confirmation"
	InteractionChoice       InteractionKind = "choice"
)

// InteractionOption is one selectable answer.
type InteractionOption struct {
	Label string `json:"label"`
}

// HumanInteraction is a mid-turn prompt routed through the inbound platform.
type HumanInteraction struct {
	ID       string              `json:"id,omitempty"`
	Kind     InteractionKind     `json:"kind"`
	Prompt   string              `json:"prompt"`
	Options  []InteractionOption `json:"options,omitempty"`
	Default  string              `json:"default,omitempty"`
	Multiple bool                `json:"multiple,omitempty"`
}

// InteractionReply is the raw human response before option matching.
type InteractionReply struct {
	Text string `json:"text"`
}

// InteractionResult is the resolved outcome of RunInteraction.
type InteractionResult struct {
	Answered bool   `json:"answered"`
	Text     string `json:"text,omitempty"`
	Selected int    `json:"selected"`
	Reason   string `json:"reason,omitempty"`
}

// SessionInteraction runs a blocking human-in-the-loop prompt on the session
// that owns the current turn. Loop attaches this via KeySessionControl.
type SessionInteraction interface {
	RunInteraction(context.Context, HumanInteraction) (InteractionResult, error)
}

// InteractionHandler is implemented by interactive platforms (CLI) that read
// replies synchronously during a turn. Async IM platforms omit this and rely on
// DeliverInteractionReply when inbound messages arrive.
type InteractionHandler interface {
	ReadInteractionReply(context.Context, HumanInteraction) (InteractionReply, error)
}

// AsyncInteractionPlatform marks IM transports that collect replies through
// inbound messages rather than a synchronous InteractionHandler.
type AsyncInteractionPlatform interface {
	AsyncInteraction() bool
}

const (
	// KeyInteractionHandler is the optional sync reply reader for the inbound
	// platform. Loop sets it from runner Platform when implemented.
	KeyInteractionHandler contextKey = "agentkit.interaction_handler"
	// KeyAsyncInteraction is true when the inbound platform delivers interaction
	// replies asynchronously via TryDeliverInteraction.
	KeyAsyncInteraction contextKey = "agentkit.async_interaction"
)

// InteractionStartPayload is emitted on interaction/start.
type InteractionStartPayload struct {
	ID       string              `json:"id"`
	Kind     InteractionKind     `json:"kind"`
	Prompt   string              `json:"prompt"`
	Options  []InteractionOption `json:"options,omitempty"`
	Default  string              `json:"default,omitempty"`
	Multiple bool                `json:"multiple,omitempty"`
}

// InteractionEndPayload closes an interaction/start.
type InteractionEndPayload struct {
	ID       string `json:"id"`
	Answered bool   `json:"answered"`
	Text     string `json:"text,omitempty"`
	Selected int    `json:"selected"`
	Reason   string `json:"reason,omitempty"`
}

// MatchInteractionOption resolves typed answers against Options.
func MatchInteractionOption(text string, options []InteractionOption) InteractionResult {
	if len(options) == 0 {
		return InteractionResult{Answered: true, Text: text, Selected: -1}
	}
	labels := make([]string, len(options))
	for i, opt := range options {
		labels[i] = opt.Label
	}
	got := matchOptionLabels(text, labels)
	return InteractionResult{
		Answered: got.Answered,
		Text:     got.text,
		Selected: got.selected,
	}
}

type matchedOption struct {
	Answered bool
	text     string
	selected int
}
