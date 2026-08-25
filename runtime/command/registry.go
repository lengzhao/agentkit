package command

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/lengzhao/agentkit"
)

// Config controls which contributed commands are exposed.
// Allow and Deny match command names and aliases case-insensitively.
// When Allow is empty, every command is eligible unless denied.
// When Allow is non-empty, only listed commands are registered.
type Config struct {
	Allow []string `json:"allow,omitempty"`
	Deny  []string `json:"deny,omitempty"`
}

type Deps struct{}

// Registry collects slash commands contributed by built plugins.
type Registry struct {
	cfg    Config
	byName map[string]agentkit.Command
	cmds   []agentkit.Command
}

// New builds an empty command registry. Runner populates providers via SetCommands.
func New(cfg Config, _ Deps) (agentkit.Commands, error) {
	return &Registry{
		cfg:    cfg,
		byName: make(map[string]agentkit.Command),
	}, nil
}

// SetCommands registers commands from every built CommandProvider.
func (r *Registry) SetCommands(providers []agentkit.CommandProvider) error {
	r.byName = make(map[string]agentkit.Command)
	r.cmds = nil
	for _, provider := range providers {
		if provider == nil {
			continue
		}
		for _, cmd := range provider.Commands() {
			if cmd == nil {
				continue
			}
			if !r.cfg.allowed(cmd.Name(), cmd.Alias()) {
				continue
			}
			if err := r.register(cmd); err != nil {
				return err
			}
		}
	}
	return nil
}

// NewFromProviders builds a registry from explicit command providers.
func NewFromProviders(cfg Config, providers []agentkit.CommandProvider) (*Registry, error) {
	r := &Registry{
		cfg:    cfg,
		byName: make(map[string]agentkit.Command),
	}
	if err := r.SetCommands(providers); err != nil {
		return nil, err
	}
	return r, nil
}

func (c Config) allowed(name, alias string) bool {
	name = normalizeName(name)
	alias = normalizeName(alias)
	if len(c.Allow) > 0 {
		for _, item := range c.Allow {
			key := normalizeName(item)
			if key == name || (alias != "" && key == alias) {
				return true
			}
		}
		return false
	}
	for _, item := range c.Deny {
		key := normalizeName(item)
		if key == name || (alias != "" && key == alias) {
			return false
		}
	}
	return true
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

var (
	_ agentkit.Commands         = (*Registry)(nil)
	_ agentkit.CommandCollector = (*Registry)(nil)
)

func normalizeName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}
