package agent

import (
	"context"
	"strings"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/helpdoc"
)

func (r *Runtime) Commands() []agentkit.Command {
	return []agentkit.Command{HelpCommand()}
}

// HelpCommand exposes the agent catalog help slash command.
func HelpCommand() agentkit.Command { return agentHelpCommand{} }

type agentHelpCommand struct{}

func (agentHelpCommand) Name() string        { return "agent" }
func (agentHelpCommand) Alias() string       { return "" }
func (agentHelpCommand) Description() string { return "list agent kinds or show agent godoc" }

func (agentHelpCommand) CommandExec(_ context.Context, args ...string) (string, error) {
	if len(args) == 0 || args[0] == "-l" || args[0] == "--list" {
		return helpdoc.FormatKindList("Registered agent kinds", helpdoc.AgentKindPrefix, "agent", "<name>"), nil
	}
	name := strings.TrimSpace(strings.Join(args, " "))
	return helpdoc.KindDoc(helpdoc.AgentKindPrefix, name)
}
