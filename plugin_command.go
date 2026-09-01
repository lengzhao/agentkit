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

// ErrCommandForbidden means the caller is not allowed to run the command.
var ErrCommandForbidden = errors.New("command forbidden")

// SlashAdminContext enriches slash command ctx (for example KeyIsAdmin).
type SlashAdminContext interface {
	EnrichSlashContext(ctx context.Context) context.Context
}

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
