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

// EffectiveTimeout resolves the wait limit for an interactive permission request.
func EffectiveTimeout(req Request, cap Capability) time.Duration {
	if req.Timeout > 0 {
		return req.Timeout
	}
	if cap.DefaultTimeout > 0 {
		return cap.DefaultTimeout
	}
	if cap.Interactive {
		return DefaultTimeout
	}
	return 0
}

func NoHuman(req Request, reason string) Result {
	switch req.Kind {
	case KindAllowDeny:
		return Result{
			Outcome:  OutcomeNoHuman,
			Allow:    false,
			Reason:   reason,
			Guidance: "No interactive user is available; treat this tool call as denied and continue without it.",
		}
	default:
		return Result{
			Outcome:  OutcomeNoHuman,
			Reason:   reason,
			Guidance: "No interactive user is available; state your assumptions and continue.",
		}
	}
}

func TimedOut(req Request) Result {
	switch req.Kind {
	case KindAllowDeny:
		return Result{
			Outcome:  OutcomeTimeout,
			Allow:    false,
			Reason:   "permission timed out",
			Guidance: "The user did not respond in time; treat this tool call as denied and continue.",
		}
	default:
		return Result{
			Outcome:  OutcomeTimeout,
			Reason:   "permission timed out",
			Guidance: "The user did not respond in time; state your assumptions and continue.",
		}
	}
}

func Cancelled(req Request, reason string) Result {
	if reason == "" {
		reason = "permission cancelled"
	}
	switch req.Kind {
	case KindAllowDeny:
		return Result{
			Outcome: OutcomeCancelled,
			Allow:   false,
			Reason:  reason,
		}
	default:
		return Result{
			Outcome:  OutcomeCancelled,
			Reason:   reason,
			Guidance: "The user declined to answer; state your assumptions and continue.",
		}
	}
}

func Superseded(req Request, reason string) Result {
	if reason == "" {
		reason = "superseded by new inbound message"
	}
	switch req.Kind {
	case KindAllowDeny:
		return Result{
			Outcome: OutcomeSuperseded,
			Allow:   false,
			Reason:  reason,
		}
	default:
		return Result{
			Outcome:  OutcomeSuperseded,
			Reason:   reason,
			Guidance: "The user sent a new message instead of answering; state your assumptions and continue.",
		}
	}
}

// RequestPayload is emitted on permission/request.
type RequestPayload struct {
	Request
}
