package subagent

import (
	"context"
	"strings"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/workspace"
)

func (s *Spawner) Commands() []agentkit.Command {
	return []agentkit.Command{HelpCommand(s.workspace, s.dirs)}
}

// HelpCommand exposes the subagent definition help slash command.
func HelpCommand(ws workspace.Service, dirs []string) agentkit.Command {
	if len(dirs) == 0 {
		dirs = DefaultDefinitionDirs()
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

func (c subagentHelpCommand) CommandExec(ctx context.Context, args ...string) (string, error) {
	if len(args) == 0 || args[0] == "-l" || args[0] == "--list" {
		return formatDefinitionList(ctx, c.workspace, c.dirs)
	}
	name := strings.TrimSpace(strings.Join(args, " "))
	return definitionDoc(ctx, c.workspace, c.dirs, name)
}
