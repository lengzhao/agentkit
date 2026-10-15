package subagent

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/subagent"
	"github.com/lengzhao/agentkit/cap/workspace"
	"github.com/lengzhao/agentkit/runtime/session/sessbind"
)

// ModelCommand exposes the /submodel slash command: workspace-wide global
// model binds for in-process subagents (models["sub:<name>"] and the
// models["sub:*"] wildcard). Session-scoped model binds never target
// subagents, so the command has no session store and no -g flag — every
// read/write goes through the workspace overlay.
func ModelCommand(ws workspace.Service, spawner subagent.Spawner) agentkit.Command {
	return submodelCommand{workspace: ws, spawner: spawner}
}

type submodelCommand struct {
	workspace workspace.Service
	spawner   subagent.Spawner
}

func (submodelCommand) Name() string  { return "submodel" }
func (submodelCommand) Alias() string { return "" }
func (submodelCommand) Description() string {
	return "show or set the global LLM model for subagents (<name|*> [model|reset])"
}

func (c submodelCommand) CommandExec(ctx context.Context, args string) (string, error) {
	fields := strings.Fields(strings.TrimSpace(args))
	// Tolerate a leading -g from the pre-/submodel habit; binds are always global.
	if len(fields) > 0 && (fields[0] == "-g" || fields[0] == "--global") {
		fields = fields[1:]
	}

	defs := c.definitions(ctx)
	if len(fields) == 0 {
		return c.showAll(ctx, defs)
	}

	key, display, err := c.target(defs, fields[0])
	if err != nil {
		return "", err
	}
	rest := strings.TrimSpace(strings.Join(fields[1:], " "))
	if rest == "" {
		return c.showOne(ctx, defs, key, display)
	}
	if strings.EqualFold(rest, "reset") || strings.EqualFold(rest, "default") {
		if err := sessbind.SetGlobalModelBind(ctx, c.workspace, key, ""); err != nil {
			return "", err
		}
		return fmt.Sprintf("subagent global model reset (%s)", display), nil
	}
	if err := sessbind.SetGlobalModelBind(ctx, c.workspace, key, rest); err != nil {
		return "", err
	}
	return fmt.Sprintf("subagent global model (%s): %s", display, rest), nil
}

// definitions loads subagent definitions for name validation and listing.
func (c submodelCommand) definitions(ctx context.Context) []subagent.Definition {
	if c.spawner == nil {
		return nil
	}
	defs, err := c.spawner.Definitions(ctx)
	if err != nil {
		slog.Warn("/submodel: load subagent definitions failed", "err", err)
		return nil
	}
	return defs
}

// target maps a user-typed target to the models-map key and display label,
// validating the name against known definitions when available so a typo can
// never silently create a dead entry.
func (c submodelCommand) target(defs []subagent.Definition, name string) (agentkit.AgentID, string, error) {
	name = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(name), "sub:"))
	if strings.EqualFold(name, "*") || strings.EqualFold(name, "all") {
		return sessbind.SubagentModelWildcardKey, "all subagents (*)", nil
	}
	if len(defs) > 0 {
		for _, def := range defs {
			if strings.EqualFold(def.Name, name) {
				return agentkit.AgentID("sub:" + def.Name), def.Name, nil
			}
		}
		return "", "", fmt.Errorf("unknown subagent %q; available: %s", name, definitionNames(defs))
	}
	// No spawner wired: accept the name as-is, keyed by convention.
	return agentkit.AgentID("sub:" + name), name, nil
}

func definitionNames(defs []subagent.Definition) string {
	names := make([]string, 0, len(defs))
	for _, def := range defs {
		names = append(names, def.Name)
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}

func findDefinition(defs []subagent.Definition, name string) *subagent.Definition {
	for i := range defs {
		if strings.EqualFold(defs[i].Name, name) {
			return &defs[i]
		}
	}
	return nil
}

func (c submodelCommand) showAll(ctx context.Context, defs []subagent.Definition) (string, error) {
	binds, err := sessbind.GlobalModelBinds(ctx, c.workspace)
	if err != nil {
		return "", err
	}

	var b strings.Builder
	b.WriteString("subagent models:\n")
	printed := false

	if wild := binds[string(sessbind.SubagentModelWildcardKey)]; wild != "" {
		fmt.Fprintf(&b, "  all subagents (*): %s\n", wild)
		printed = true
	}

	seen := map[string]bool{string(sessbind.SubagentModelWildcardKey): true}
	names := make([]string, 0, len(defs))
	for _, def := range defs {
		names = append(names, def.Name)
	}
	sort.Strings(names)
	for _, name := range names {
		seen["sub:"+name] = true
		bound := binds["sub:"+name]
		printed = true
		if bound == "" {
			bound = "(not set)"
		}
		if def := findDefinition(defs, name); def != nil && def.Model != "" {
			bound = fmt.Sprintf("%s (md model: %s takes precedence)", bound, def.Model)
		}
		fmt.Fprintf(&b, "  %s: %s\n", name, bound)
	}

	// Stale entries: sub: keys that no longer match any definition.
	var stale []string
	for key := range binds {
		if strings.HasPrefix(key, "sub:") && !seen[key] {
			stale = append(stale, key)
		}
	}
	sort.Strings(stale)
	for _, key := range stale {
		printed = true
		fmt.Fprintf(&b, "  %s: %s (definition not found)\n", strings.TrimPrefix(key, "sub:"), binds[key])
	}

	if !printed {
		b.WriteString("  (none configured)\n")
	}
	b.WriteString("Use /submodel <name|*> <model> to set, /submodel <name|*> reset to clear.")
	return strings.TrimRight(b.String(), "\n"), nil
}

func (c submodelCommand) showOne(ctx context.Context, defs []subagent.Definition, key agentkit.AgentID, display string) (string, error) {
	bound, err := sessbind.GlobalModelBind(ctx, c.workspace, key)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	if bound != "" {
		fmt.Fprintf(&b, "subagent %s global model: %s\n", display, bound)
	} else {
		fmt.Fprintf(&b, "subagent %s global model: (not set)\n", display)
	}
	if def := findDefinition(defs, strings.TrimPrefix(string(key), "sub:")); def != nil && def.Model != "" {
		fmt.Fprintf(&b, "definition model (agents/%s.md): %s — takes precedence\n", def.Name, def.Model)
	}
	b.WriteString("agents/<name>.md 的 model: 优先于此设置。")
	b.WriteString("\nUse /submodel <name|*> <model> to set, /submodel <name|*> reset to clear.")
	return b.String(), nil
}
