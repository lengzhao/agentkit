package subagent

import (
	"context"
	"strings"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/workspace"
	"github.com/lengzhao/agentkit/runtime/subagent/definition"
)

// Commands implements agentkit.CommandProvider: subagent/inprocess contributes
// its own slash commands (/subagent, /submodel), collected by commands/registry
// through the generic WireContributions channel.
func (s *Spawner) Commands() []agentkit.Command {
	return []agentkit.Command{HelpCommand(s.workspace, s.dirs), ModelCommand(s.workspace, s)}
}

// HelpCommand exposes the subagent definition help slash command.
func HelpCommand(ws workspace.Service, dirs []string) agentkit.Command {
	if len(dirs) == 0 {
		dirs = definition.DefaultDirs()
	}
	return subagentHelpCommand{workspace: ws, dirs: dirs}
}

type subagentHelpCommand struct {
	workspace workspace.Service
	dirs      []string
}

func (subagentHelpCommand) Name() string        { return "subagent" }
func (subagentHelpCommand) Alias() string       { return "" }
func (subagentHelpCommand) Description() string { return "list subagent definitions or show details" }

func (c subagentHelpCommand) CommandExec(ctx context.Context, args string) (string, error) {
	fields := strings.Fields(strings.TrimSpace(args))
	if len(fields) == 0 || fields[0] == "-l" || fields[0] == "--list" {
		return definition.FormatList(ctx, c.workspace, c.dirs)
	}
	return definition.Doc(ctx, c.workspace, c.dirs, strings.TrimSpace(args))
}
