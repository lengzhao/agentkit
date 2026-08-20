package agentkit

import "context"

type Command interface {
	Name() string
	Description() string
	Run(context.Context, CommandRequest) (CommandResult, error)
}

type CommandRegistry interface {
	Register(Command) error
}

type CommandRequest struct {
	SessionID SessionID
	AgentID   AgentID
	Name      string
	Args      []string
}

type CommandResult struct {
	Events []OutboundEvent
}
