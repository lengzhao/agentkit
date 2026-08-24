package agentkit

import "context"

// CommandProvider contributes human-facing commands from a built plugin
// instance. It is designed to work with pluginkit/build.Collect so commands
// can live next to the capability that owns their behavior.
type CommandProvider interface {
	Commands() []Command
}

// Command is one slash command contribution.
type Command interface {
	Name() string
	Alias() string
	Description() string
	CommandExec(ctx context.Context, args ...string) (string, error)
}

// CommandResult is the outcome of a handled slash command.
type CommandResult struct {
	Output     string
	NewSession SessionID
}

// Commands is a post-build slash command catalog for platforms.
type Commands interface {
	Dispatch(ctx context.Context, name string, args []string) (*CommandResult, error)
	List() []Command
}

// CommandCollector receives CommandProvider contributions after pluginkit build.
// Runner calls SetCommands on every built commands/registry instance.
type CommandCollector interface {
	SetCommands(providers []CommandProvider) error
}
