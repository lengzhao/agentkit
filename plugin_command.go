package agentkit

import (
	"context"
	"errors"
)

// CommandProvider contributes human-facing commands from a built plugin
// instance. It is designed to work with pluginkit/build.WireContributions so
// commands can live next to the capability that owns their behavior.
type CommandProvider interface {
	Commands() []Command
}

// Command is one slash command contribution.
type Command interface {
	Name() string
	Alias() string
	Description() string
	CommandExec(ctx context.Context, args string) (string, error)
}

// ErrCommandNotHandled means the name is not a registered slash command.
var ErrCommandNotHandled = errors.New("command not handled")

// Commands is a post-build slash command catalog for platforms.
type Commands interface {
	Dispatch(ctx context.Context, name string, rawArgs string) (string, error)
	List() []Command
}

// CommandCollector receives CommandProvider contributions after pluginkit build.
// Runner wires them with build.WireContributions during Run.
type CommandCollector interface {
	SetCommands(providers []CommandProvider) error
}
