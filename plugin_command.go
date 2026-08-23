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
