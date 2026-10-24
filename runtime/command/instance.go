package command

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/lengzhao/agentkit"
)

// instanceCommand exposes resolved names while forwarding execution and redaction.
type instanceCommand struct {
	agentkit.Command
	original      agentkit.Command
	name, alias   string
	resolutionErr error
}

func (c instanceCommand) Name() string  { return c.name }
func (c instanceCommand) Alias() string { return c.alias }
func (c instanceCommand) SanitizeArgsForLog(args string) string {
	if sanitizer, ok := c.original.(agentkit.CommandLogSanitizer); ok {
		return sanitizer.SanitizeArgsForLog(args)
	}
	return args
}

func resolveInstanceCommands(providers []agentkit.CommandProvider) ([]instanceCommand, error) {
	type contribution struct {
		command agentkit.Command
		id      string
	}
	var contributions []contribution
	counts := make(map[string]int)
	for _, provider := range providers {
		if provider == nil {
			continue
		}
		id := ""
		if identified, ok := provider.(interface{ CommandInstanceID() string }); ok {
			id = normalizeName(identified.CommandInstanceID())
		}
		for _, cmd := range provider.Commands() {
			if cmd == nil {
				continue
			}
			name, alias := normalizeName(cmd.Name()), normalizeName(cmd.Alias())
			counts[name]++
			if alias != "" && alias != name {
				counts[alias]++
			}
			contributions = append(contributions, contribution{cmd, id})
		}
	}
	var commands []instanceCommand
	for _, entry := range contributions {
		resolve := func(name string) (string, error) {
			name = normalizeName(name)
			if name == "" || counts[name] < 2 {
				return name, nil
			}
			if entry.id == "" || strings.ContainsAny(entry.id, "/\\@") || strings.ContainsFunc(entry.id, unicode.IsSpace) {
				return name, fmt.Errorf("conflicting command %q requires a usable plugin instance ID, got %q", name, entry.id)
			}
			return name + "_" + entry.id, nil
		}
		name, err := resolve(entry.command.Name())
		alias, aliasErr := resolve(entry.command.Alias())
		if err == nil {
			err = aliasErr
		}
		if alias == name {
			alias = ""
		}
		commands = append(commands, instanceCommand{Command: entry.command, original: entry.command, name: name, alias: alias, resolutionErr: err})
	}
	return commands, nil
}
