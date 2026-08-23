package command

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/pluginkit/build"
)

// Registry collects slash commands contributed by built plugins.
type Registry struct {
	byName map[string]agentkit.Command
	cmds   []agentkit.Command
}

// NewFromProviders builds a registry from explicit command providers.
func NewFromProviders(providers []agentkit.CommandProvider) (*Registry, error) {
	r := &Registry{byName: make(map[string]agentkit.Command)}
	for _, provider := range providers {
		if provider == nil {
			continue
		}
		for _, cmd := range provider.Commands() {
			if cmd == nil {
				continue
			}
			if err := r.register(cmd); err != nil {
				return nil, err
			}
		}
	}
	return r, nil
}

// CollectFromBuild gathers every built instance that implements
// agentkit.CommandProvider and registers its commands.
func CollectFromBuild(result *build.Result) (*Registry, error) {
	return NewFromProviders(build.Collect[agentkit.CommandProvider](result))
}

func (r *Registry) register(cmd agentkit.Command) error {
	name := normalizeName(cmd.Name())
	if name == "" {
		return fmt.Errorf("command name is required")
	}
	if _, exists := r.byName[name]; exists {
		return fmt.Errorf("command already registered: %s", name)
	}
	r.byName[name] = cmd
	if alias := normalizeName(cmd.Alias()); alias != "" {
		if _, exists := r.byName[alias]; exists {
			return fmt.Errorf("command alias already registered: %s", alias)
		}
		r.byName[alias] = cmd
	}
	r.cmds = append(r.cmds, cmd)
	return nil
}

func (r *Registry) Dispatch(ctx context.Context, name string, args []string) (*agentkit.CommandResult, error) {
	key := normalizeName(name)
	if key == "" {
		return nil, nil
	}
	cmd, ok := r.byName[key]
	if !ok {
		return nil, nil
	}
	out, err := cmd.CommandExec(ctx, args...)
	if err != nil {
		return nil, err
	}
	result := &agentkit.CommandResult{Output: out}
	if cmd.Name() == "new" && out != "" {
		result.NewSession = agentkit.SessionID(out)
	}
	return result, nil
}

func (r *Registry) List() []agentkit.Command {
	out := make([]agentkit.Command, len(r.cmds))
	copy(out, r.cmds)
	sort.Slice(out, func(i, j int) bool {
		return out[i].Name() < out[j].Name()
	})
	return out
}

var _ agentkit.Commands = (*Registry)(nil)

func normalizeName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}
