// Package permission is the capability boundary for human-in-the-loop
// decisions routed through the inbound platform (tool approval, ask_user, etc.).
package permission

import (
	"context"
	"time"

	"github.com/lengzhao/agentkit"
)

type Kind string

const (
	KindAllowDeny Kind = "allow_deny"
	KindQuestion  Kind = "question"
)

// DefaultTimeout is the wait limit when Request.Timeout and Capability.DefaultTimeout are both unset.
const DefaultTimeout = 10 * time.Minute

type Option struct {
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
}

type Question struct {
	Prompt      string   `json:"prompt"`
	Header      string   `json:"header,omitempty"`
	Options     []Option `json:"options,omitempty"`
	Default     string   `json:"default,omitempty"`
	MultiSelect bool     `json:"multiSelect,omitempty"`
}

type Request struct {
	ID       string             `json:"id"`
	Kind     Kind               `json:"kind"`
	Reason   string             `json:"reason,omitempty"`
	ToolCall *agentkit.ToolCall `json:"toolCall,omitempty"`
	Question *Question          `json:"question,omitempty"`
	Timeout  time.Duration      `json:"timeout,omitempty"`
	AskedBy  string             `json:"askedBy,omitempty"`
}

type Outcome string

const (
	OutcomeResolved   Outcome = "resolved"
	OutcomeTimeout    Outcome = "timeout"
	OutcomeNoHuman    Outcome = "no_human"
	OutcomeCancelled  Outcome = "cancelled"
	OutcomeSuperseded Outcome = "superseded"
)

type QuestionResult struct {
	Text     string `json:"text,omitempty"`
	Selected []int  `json:"selected,omitempty"`
}

// Result is the outcome of Broker.Await. ID is set on permission/resolved events.
type Result struct {
	ID           string          `json:"id,omitempty"`
	Outcome      Outcome         `json:"outcome"`
	Allow        bool            `json:"allow,omitempty"`
	Answer       *QuestionResult `json:"answer,omitempty"`
	UpdatedInput map[string]any  `json:"updatedInput,omitempty"`
	Reason       string          `json:"reason,omitempty"`
	Guidance     string          `json:"guidance,omitempty"`
}

func (r Result) Resolved() bool { return r.Outcome == OutcomeResolved }

type Broker interface {
	Await(context.Context, Request) (Result, error)
}

// RequestPayload is emitted on permission/request.
type RequestPayload struct {
	Request
}

// AllowDenyMatch is the parsed allow/deny reply.
type AllowDenyMatch struct {
	Allow        bool
	Recognized   bool
	UpdatedInput map[string]any
}
