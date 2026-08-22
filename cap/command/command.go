package command

import (
	"context"
	"io"

	"github.com/lengzhao/agentkit"
)

// Registry collects slash command handlers and dispatches by name or alias.
type Registry interface {
	Register(Descriptor, Handler) error
	Dispatch(context.Context, Request) (Result, error)
	List() []Descriptor
}

// Handler executes one slash command without involving the model.
type Handler interface {
	Handle(context.Context, Request) (Result, error)
}

type Descriptor struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Aliases     []string `json:"aliases,omitempty"`
}

type Request struct {
	Name       string
	Args       string
	SessionID  agentkit.SessionID
	AgentID    agentkit.AgentID
	PlatformID string
	ErrOut     io.Writer
	Out        io.Writer
}

type Result struct {
	Handled    bool
	NewSession agentkit.SessionID
}
