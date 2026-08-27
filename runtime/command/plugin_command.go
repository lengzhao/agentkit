package command

import (
	"context"
	"fmt"
	"strings"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/helpdoc"
)

func (r *Registry) Commands() []agentkit.Command {
	return []agentkit.Command{pluginCommand{}}
}

// PluginHelpCommand exposes the plugin catalog help slash command.
func PluginHelpCommand() agentkit.Command { return pluginCommand{} }

type pluginCommand struct{}

func (pluginCommand) Name() string        { return "plugin" }
func (pluginCommand) Alias() string       { return "" }
func (pluginCommand) Description() string { return "list plugin kinds or show plugin godoc" }

func (pluginCommand) CommandExec(_ context.Context, args ...string) (string, error) {
	if len(args) == 0 {
		return "", fmt.Errorf("usage: /plugin -l\n       /plugin <kind>")
	}
	switch args[0] {
	case "-l", "--list":
		return helpdoc.FormatKindList("Registered plugin kinds", "", "plugin", "<kind>"), nil
	}
	kind := strings.TrimSpace(strings.Join(args, " "))
	return helpdoc.KindDoc("", kind)
}
