package command

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/session"
)

// Config controls which contributed commands are exposed.
// Allow and Deny match command names and aliases case-insensitively.
// When Allow is empty, every command is eligible unless denied.
// When Allow is non-empty, only listed commands are registered.
type Config struct {
	// Allow exposes only these command names; empty means all.
	Allow []string `json:"allow,omitempty"`
	// Deny hides these command names; applied after Allow.
	Deny []string `json:"deny,omitempty"`
	// Admins lists user IDs that may run admin-only slash commands. Matching is
	// case-insensitive. When empty, admin restrictions are not enforced.
	Admins []string `json:"admins,omitempty"`
	// AdminOnly lists command names and aliases that require an admin user when
	// Admins is non-empty.
	AdminOnly []string `json:"adminOnly,omitempty"`
}

type Deps struct {
	// SessionCommands pulls session lifecycle slash commands into the build graph.
	SessionCommands agentkit.CommandProvider `json:"sessionCommands,omitempty"`
}

// Registry collects slash commands contributed by built plugins.
type Registry struct {
	cfg    Config
	byName map[string]agentkit.Command
	cmds   []agentkit.Command
}

// New registers commands/registry: Aggregate slash commands contributed by CommandProvider plugins.
//
// Best practices:
//   - Providers are discovered from the built graph, so a command appears as soon as its plugin is wired in.
func New(cfg Config, deps Deps) (agentkit.Commands, error) {
	_ = deps.SessionCommands
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

func (r *Registry) Dispatch(ctx context.Context, name string, rawArgs string) (string, error) {
	ctx = r.EnrichSlashContext(ctx)
	key := normalizeName(name)
	if key == "" {
		return "", agentkit.ErrCommandNotHandled
	}
	cmd, ok := r.byName[key]
	if !ok {
		return "", agentkit.ErrCommandNotHandled
	}
	if r.requiresAdmin(key, cmd) && !IsAdmin(ctx) {
		slog.Warn("command forbidden", commandLogAttrs(ctx, cmd.Name(), rawArgs)...)
		return "", agentkit.ErrCommandForbidden
	}
	slog.Info("command execute", commandLogAttrs(ctx, cmd.Name(), rawArgs)...)
	out, err := cmd.CommandExec(ctx, rawArgs)
	if err != nil {
		slog.Error("command failed", commandLogAttrs(ctx, cmd.Name(), rawArgs, "err", err)...)
	}
	return out, err
}

// EnrichSlashContext sets KeyIsAdmin when the current user matches Config.Admins.
func (r *Registry) EnrichSlashContext(ctx context.Context) context.Context {
	if len(r.cfg.Admins) == 0 {
		return ctx
	}
	userID := session.UserIDFromContext(ctx)
	if !isAdminUser(r.cfg.Admins, userID) {
		return ctx
	}
	return context.WithValue(ctx, agentkit.KeyIsAdmin, true)
}

func (r *Registry) requiresAdmin(dispatchName string, cmd agentkit.Command) bool {
	if len(r.cfg.Admins) == 0 || len(r.cfg.AdminOnly) == 0 {
		return false
	}
	candidates := []string{
		normalizeName(dispatchName),
		normalizeName(cmd.Name()),
		normalizeName(cmd.Alias()),
	}
	for _, item := range r.cfg.AdminOnly {
		key := normalizeName(item)
		for _, name := range candidates {
			if name != "" && key == name {
				return true
			}
		}
	}
	return false
}

func isAdminUser(admins []string, userID string) bool {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return false
	}
	for _, id := range admins {
		if strings.EqualFold(strings.TrimSpace(id), userID) {
			return true
		}
	}
	return false
}

func (r *Registry) List(ctx context.Context) []agentkit.Command {
	ctx = r.EnrichSlashContext(ctx)
	out := make([]agentkit.Command, 0, len(r.cmds))
	for _, cmd := range r.cmds {
		if r.requiresAdmin(normalizeName(cmd.Name()), cmd) && !IsAdmin(ctx) {
			continue
		}
		out = append(out, cmd)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Name() < out[j].Name()
	})
	return out
}

var (
	_ agentkit.Commands           = (*Registry)(nil)
	_ agentkit.CommandCollector   = (*Registry)(nil)
	_ agentkit.CommandProvider    = (*Registry)(nil)
	_ agentkit.SlashAdminContext  = (*Registry)(nil)
)

func normalizeName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

func commandLogAttrs(ctx context.Context, name, rawArgs string, extra ...any) []any {
	attrs := []any{
		"command", strings.TrimSpace(name),
		"user_id", session.UserIDFromContext(ctx),
		"platform_id", session.PlatformFromContext(ctx),
		"session_id", session.ConversationFromContext(ctx),
		"delivery_session_id", session.DeliveryRouteFromContext(ctx),
		"args", summarizeCommandArgs(rawArgs),
	}
	return append(attrs, extra...)
}

func summarizeCommandArgs(args string) string {
	args = strings.TrimSpace(args)
	const maxLen = 200
	if len(args) <= maxLen {
		return args
	}
	return args[:maxLen] + "…"
}
