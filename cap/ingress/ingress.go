// Package ingress defines optional, platform-neutral inbound business policy.
// Policies must be concurrency-safe and honor context cancellation. They do not
// own transport validation, subscriptions, authorization or attachment storage.
package ingress

import (
	"context"
	"encoding/json"
	"fmt"
)

type Kind string

const (
	Message     Kind = "message"
	Event       Kind = "event"
	Interaction Kind = "interaction"
)

type ContextState string

const (
	Unknown  ContextState = "unknown"
	New      ContextState = "new"
	Existing ContextState = "existing"
)

type AgentAction string

const (
	Inherit AgentAction = ""
	Submit  AgentAction = "submit"
	Skip    AgentAction = "skip"
	Defer   AgentAction = "defer"
)

type Choice string

const (
	Keep    Choice = ""
	Enable  Choice = "enable"
	Disable Choice = "disable"
)

type Plan struct {
	Rejected bool
	Agent    AgentAction
	Handlers bool
}
type MessageFacts struct {
	AttachmentOnly   bool
	HasText          bool
	Mentioned        *bool // nil means bot identity unavailable
	GroupReplyAll    bool
	AddressedToAgent bool // normal mention/@all/engaged-thread rules, excluding groupReplyAll
}
type Input struct {
	Platform         string
	InstanceID       string
	Kind             Kind
	Type             string
	ID               string
	ChatID           string
	UserID           string
	ConversationKind string // group, direct, unknown
	ThreadID         string
	SessionID        string // resolved logical session, empty when unknown
	Context          ContextState
	Message          *MessageFacts
	Default          Plan
	Raw              json.RawMessage // immutable snapshot; policies must not retain or mutate it
}
type Decision struct {
	Reject   bool
	Agent    AgentAction
	Handlers Choice
	Reason   string
}
type Policy interface {
	Evaluate(context.Context, Input) (Decision, error)
}

// Resolve validates a decision and merges it with defaults. Only messages can
// enable an otherwise untriggered agent turn. Event/interaction processing must
// stay within its configured destinations. Admission was checked by the adapter.
func Resolve(in Input, d Decision) (Plan, error) {
	if d.Reject {
		return Plan{Agent: Skip, Rejected: true}, nil
	}
	p := in.Default
	switch d.Agent {
	case Inherit:
	case Skip:
		p.Agent = Skip
	case Submit:
		if in.Kind != Message && in.Default.Agent != Submit {
			return Plan{}, fmt.Errorf("agent destination is not enabled")
		}
		p.Agent = Submit
	case Defer:
		if in.Kind != Message || in.Message == nil || !in.Message.AttachmentOnly {
			return Plan{}, fmt.Errorf("defer requires attachment-only message")
		}
		p.Agent = Defer
	default:
		return Plan{}, fmt.Errorf("invalid agent action %q", d.Agent)
	}
	switch d.Handlers {
	case Keep:
	case Disable:
		p.Handlers = false
	case Enable:
		if !in.Default.Handlers {
			return Plan{}, fmt.Errorf("handler destination is not enabled")
		}
	default:
		return Plan{}, fmt.Errorf("invalid handler choice %q", d.Handlers)
	}
	return p, nil
}
