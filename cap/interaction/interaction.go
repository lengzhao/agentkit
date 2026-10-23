// Package interaction defines portable external card interactions. Permission
// replies use cap/permission exclusively and never enter this handler chain.
package interaction

import (
	"context"
	"encoding/json"
)

type CardStatus string

const (
	CardAvailable   CardStatus = "available"
	CardUnavailable CardStatus = "unavailable"
	CardFailed      CardStatus = "failed"
)

// Answer retains the original value even when a display label can be resolved.
// Control is the card control name/action_id (or form field name).
type Answer struct {
	Control string `json:"control,omitempty"`
	Value   any    `json:"value"`
	Label   string `json:"label,omitempty"`
}

// Event is a snapshot owned by one asynchronous interaction worker. RawCallback
// and RawCard retain details not represented by the portable fields.
type Event struct {
	EventID       string          `json:"event_id,omitempty"`
	InteractionID string          `json:"interaction_id,omitempty"`
	RoutingStatus string          `json:"routing_status,omitempty"`
	Platform      string          `json:"platform"`
	Type          string          `json:"type"`
	UserID        string          `json:"user_id"`
	ChatID        string          `json:"chat_id"`
	MessageID     string          `json:"message_id"`
	ThreadID      string          `json:"thread_id,omitempty"`
	SessionID     string          `json:"session_id"`
	Action        string          `json:"action,omitempty"`
	Answers       []Answer        `json:"answers,omitempty"`
	CardStatus    CardStatus      `json:"card_status"`
	CardError     string          `json:"card_error,omitempty"`
	CardText      string          `json:"card_text,omitempty"`
	RawCard       json.RawMessage `json:"raw_card,omitempty"`
	RawAction     json.RawMessage `json:"raw_action,omitempty"`
	RawCallback   json.RawMessage `json:"raw_callback,omitempty"`
}

// Handler is injected through platform deps.interactionHandlers. Handlers run
// in declaration order after builtins. Return true only when consumed. Errors
// are logged and the chain continues; a failed handler cannot suppress fallback.
// Implementations must support concurrent calls and honor ctx cancellation.
type Handler interface {
	HandleInteraction(context.Context, Event) (handled bool, err error)
}
