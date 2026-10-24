package command

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/rctx"
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
	cfg       Config
	byName    map[string]agentkit.Command
	cmds      []agentkit.Command
	ambiguous map[string][]string
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
	commands, err := resolveInstanceCommands(providers)
	if err != nil {
		return err
	}
	next := &Registry{cfg: r.cfg, byName: make(map[string]agentkit.Command), ambiguous: make(map[string][]string)}
	for _, cmd := range commands {
		if !r.cfg.allowed(cmd.original.Name(), cmd.original.Alias(), cmd.Name(), cmd.Alias()) {
			continue
		}
		if cmd.resolutionErr != nil {
			return cmd.resolutionErr
		}
		if err := next.register(cmd); err != nil {
			return err
		}
		for _, pair := range [][2]string{{normalizeName(cmd.original.Name()), cmd.Name()}, {normalizeName(cmd.original.Alias()), cmd.Alias()}} {
			if pair[0] != "" && pair[0] != pair[1] {
				next.ambiguous[pair[0]] = append(next.ambiguous[pair[0]], pair[1])
			}
		}
	}
	r.byName, r.cmds, r.ambiguous = next.byName, next.cmds, next.ambiguous
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

func (c Config) allowed(names ...string) bool {
	matches := func(items []string) bool {
		for _, item := range items {
			key := normalizeName(item)
			for _, name := range names {
				if key != "" && key == normalizeName(name) {
					return true
				}
			}
		}
		return false
	}
	return (len(c.Allow) == 0 || matches(c.Allow)) && !matches(c.Deny)
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
		var alternatives []string
		for _, name := range r.ambiguous[key] {
			candidate := r.byName[name]
			if candidate != nil && (!r.requiresAdmin(name, candidate) || IsAdmin(ctx)) {
				alternatives = append(alternatives, "/"+name)
			}
		}
		if len(alternatives) > 0 {
			sort.Strings(alternatives)
			return "", fmt.Errorf("ambiguous command /%s; use %s", key, strings.Join(alternatives, ", "))
		}
		return "", agentkit.ErrCommandNotHandled
	}
	if r.requiresAdmin(key, cmd) && !IsAdmin(ctx) {
		slog.Warn("command forbidden", r.commandLogAttrs(ctx, cmd, rawArgs)...)
		return "", agentkit.ErrCommandForbidden
	}
	logAttrs := r.commandLogAttrs(ctx, cmd, rawArgs)
	slog.Info("command execute", logAttrs...)
	out, err := cmd.CommandExec(ctx, rawArgs)
	if err != nil {
		failAttrs := append(append([]any(nil), logAttrs...), "err", err)
		slog.Error("command failed", failAttrs...)
	}
	return out, err
}

// EnrichSlashContext sets KeyIsAdmin when the current user matches Config.Admins.
func (r *Registry) EnrichSlashContext(ctx context.Context) context.Context {
	if len(r.cfg.Admins) == 0 {
		return ctx
	}
	userID := rctx.UserIDFromContext(ctx)
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
	if resolved, ok := cmd.(instanceCommand); ok {
		candidates = append(candidates, normalizeName(resolved.original.Name()), normalizeName(resolved.original.Alias()))
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
	_ agentkit.Commands          = (*Registry)(nil)
	_ agentkit.CommandCollector  = (*Registry)(nil)
	_ agentkit.CommandProvider   = (*Registry)(nil)
	_ agentkit.SlashAdminContext = (*Registry)(nil)
)

func normalizeName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

func (r *Registry) commandLogAttrs(ctx context.Context, cmd agentkit.Command, rawArgs string, extra ...any) []any {
	name := ""
	if cmd != nil {
		name = cmd.Name()
	}
	attrs := []any{
		"command", strings.TrimSpace(name),
		"user_id", rctx.UserIDFromContext(ctx),
		"platform_id", rctx.PlatformFromContext(ctx),
		"session_id", rctx.ConversationFromContext(ctx),
		"delivery_session_id", rctx.DeliveryRouteFromContext(ctx),
		"args", sanitizeArgsForLog(cmd, rawArgs),
	}
	return append(attrs, extra...)
}
