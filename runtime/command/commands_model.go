package command

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"

	"github.com/lengzhao/agentkit"
	capllm "github.com/lengzhao/agentkit/cap/llm"
	capsubagent "github.com/lengzhao/agentkit/cap/subagent"
	"github.com/lengzhao/agentkit/cap/workspace"
	"github.com/lengzhao/agentkit/runtime/session/sessbind"
)

// ModelCommandExtra carries optional collaborators for /model: subagent
// definitions for name validation and listing, and the LLM provider for the
// model catalog. Zero values degrade gracefully (no validation, no listing).
type ModelCommandExtra struct {
	Subagents capsubagent.Spawner
	LLM       agentkit.LLMProvider
}

// ModelCommand shows or sets the session or global LLM model override for coding agents.
func ModelCommand(agents []agentkit.Agent, store agentkit.SessionStore, defaultAgent agentkit.AgentID, ws workspace.Service, extra ...ModelCommandExtra) agentkit.Command {
	c := modelCommand{agents: agents, store: store, defaultAgent: defaultAgent, workspace: ws}
	if len(extra) > 0 {
		c.subagents = extra[0].Subagents
		c.llm = extra[0].LLM
	}
	return c
}

type modelCommand struct {
	agents       []agentkit.Agent
	store        agentkit.SessionStore
	defaultAgent agentkit.AgentID
	workspace    workspace.Service
	subagents    capsubagent.Spawner
	llm          agentkit.LLMProvider
}

func (modelCommand) Name() string  { return "model" }
func (modelCommand) Alias() string { return "" }
func (modelCommand) Description() string {
	return "show or set the LLM model for this session (-g for global default; -g sub <name|*> for subagents)"
}

func (c modelCommand) CommandExec(ctx context.Context, args string) (string, error) {
	global, payload, _ := parseCatalogSlashArgs(args)

	// /model -g sub [...] configures in-process subagent models. They are
	// workspace-wide explicit config; session-scoped /model never targets them.
	if found, scope := peelSubScope(payload); found {
		if !global {
			return "", fmt.Errorf("subagent models are workspace-wide; use: /model -g sub <name|*> <model>")
		}
		return c.subagentExec(ctx, scope)
	}

	if payload == "" || isModelShowPayload(payload) {
		return c.show(ctx, global)
	}
	if strings.EqualFold(payload, "reset") || strings.EqualFold(payload, "default") {
		return c.clear(ctx, global)
	}
	return c.set(ctx, global, payload)
}

// subScope is a parsed subagent target: the wildcard ("*"/"all"), a
// definition name, or an empty name for the overview listing.
type subScope struct {
	name string
	rest string
}

func (s subScope) wildcard() bool {
	return strings.EqualFold(s.name, "*") || strings.EqualFold(s.name, "all")
}

// peelSubScope recognizes the subagent grammar:
//
//	sub                  → overview (name empty)
//	sub <name|*> [rest]  → target plus remaining args
//	sub:<name|*> [rest]  → legacy alias
//
// The wildcard token is explicit so a typo'd subagent name can never be
// silently reinterpreted as a model id.
// isModelShowPayload treats common "display status" typos as bare /model.
func isModelShowPayload(payload string) bool {
	switch strings.ToLower(strings.TrimSpace(payload)) {
	case "show", "status", "list":
		return true
	default:
		return false
	}
}

func peelSubScope(payload string) (bool, subScope) {
	fields := strings.Fields(strings.TrimSpace(payload))
	if len(fields) == 0 {
		return false, subScope{}
	}
	if strings.EqualFold(fields[0], "sub") {
		if len(fields) == 1 {
			return true, subScope{}
		}
		return true, subScope{name: fields[1], rest: strings.Join(fields[2:], " ")}
	}
	if strings.HasPrefix(fields[0], "sub:") {
		name := strings.TrimSpace(strings.TrimPrefix(fields[0], "sub:"))
		return true, subScope{name: name, rest: strings.Join(fields[1:], " ")}
	}
	return false, subScope{}
}

func (c modelCommand) subagentExec(ctx context.Context, scope subScope) (string, error) {
	defs := c.subagentDefinitions(ctx)
	if scope.name == "" {
		return c.showSubagents(ctx, defs)
	}
	key, display, err := c.subagentTarget(defs, scope.name)
	if err != nil {
		return "", err
	}
	rest := strings.TrimSpace(scope.rest)
	if rest == "" {
		return c.showSubagent(ctx, defs, key, display)
	}
	if strings.EqualFold(rest, "reset") || strings.EqualFold(rest, "default") {
		if err := sessbind.SetGlobalModelBind(ctx, c.workspace, key, ""); err != nil {
			return "", err
		}
		return fmt.Sprintf("subagent global model reset (%s)", display), nil
	}
	model := rest
	if err := sessbind.SetGlobalModelBind(ctx, c.workspace, key, model); err != nil {
		return "", err
	}
	return fmt.Sprintf("subagent global model (%s): %s%s", display, model, c.unknownModelWarning(model)), nil
}

// subagentDefinitions loads subagent definitions when a spawner is wired.
func (c modelCommand) subagentDefinitions(ctx context.Context) []capsubagent.Definition {
	if c.subagents == nil {
		return nil
	}
	defs, err := c.subagents.Definitions(ctx)
	if err != nil {
		slog.Warn("/model: load subagent definitions failed", "err", err)
		return nil
	}
	return defs
}

// subagentTarget maps a user-typed target to the models-map key and display
// label, validating the name against known definitions when available so a
// typo can never silently create a dead entry.
func (c modelCommand) subagentTarget(defs []capsubagent.Definition, name string) (agentkit.AgentID, string, error) {
	scope := subScope{name: name}
	if scope.wildcard() {
		return sessbind.SubagentModelWildcardKey, "all subagents (*)", nil
	}
	if len(defs) > 0 {
		for _, def := range defs {
			if strings.EqualFold(def.Name, name) {
				return agentkit.AgentID("sub:" + def.Name), def.Name, nil
			}
		}
		return "", "", fmt.Errorf("unknown subagent %q; available: %s", name, subagentNamesOf(defs))
	}
	// No spawner wired: accept the name as-is, keyed by convention.
	return agentkit.AgentID("sub:" + name), name, nil
}

func subagentNamesOf(defs []capsubagent.Definition) string {
	names := make([]string, 0, len(defs))
	for _, def := range defs {
		names = append(names, def.Name)
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}

func (c modelCommand) showSubagents(ctx context.Context, defs []capsubagent.Definition) (string, error) {
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
		if def := findDef(defs, name); def != nil && def.Model != "" {
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
	b.WriteString("Use /model -g sub <name|*> <model> to set, /model -g sub <name|*> reset to clear.")
	return strings.TrimRight(b.String(), "\n"), nil
}

func findDef(defs []capsubagent.Definition, name string) *capsubagent.Definition {
	for i := range defs {
		if strings.EqualFold(defs[i].Name, name) {
			return &defs[i]
		}
	}
	return nil
}

func (c modelCommand) showSubagent(ctx context.Context, defs []capsubagent.Definition, key agentkit.AgentID, display string) (string, error) {
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
	if def := findDef(defs, strings.TrimPrefix(string(key), "sub:")); def != nil && def.Model != "" {
		fmt.Fprintf(&b, "definition model (agents/%s.md): %s — takes precedence\n", def.Name, def.Model)
	}
	b.WriteString("agents/<name>.md 的 model: 优先于此设置。")
	b.WriteString("\nUse /model -g sub <name|*> <model> to set, /model -g sub <name|*> reset to clear.")
	return b.String(), nil
}

// catalogIDs lists catalog model ids when an LLM provider implementing
// capllm.ModelCatalog is wired.
func (c modelCommand) catalogIDs() []string {
	if c.llm == nil {
		return nil
	}
	cat, ok := c.llm.(capllm.ModelCatalog)
	if !ok {
		return nil
	}
	entries := cat.CatalogModels()
	ids := make([]string, 0, len(entries))
	for _, e := range entries {
		if id := strings.TrimSpace(e.ID); id != "" {
			ids = append(ids, id)
		}
	}
	return ids
}

// unknownModelWarning returns a non-blocking hint when the model is absent
// from the catalog; custom baseUrl models remain legal, so never block.
func (c modelCommand) unknownModelWarning(model string) string {
	ids := c.catalogIDs()
	if len(ids) == 0 {
		return ""
	}
	for _, id := range ids {
		if strings.EqualFold(id, model) {
			return ""
		}
	}
	return fmt.Sprintf("\nwarning: %q is not in the model catalog (available: %s)", model, strings.Join(ids, ", "))
}

func (c modelCommand) catalogRoutingDeps() catalogRoutingDeps {
	return catalogRoutingDeps{
		store:        c.store,
		defaultAgent: c.defaultAgent,
		workspace:    c.workspace,
	}
}

func (c modelCommand) show(ctx context.Context, globalOnly bool) (string, error) {
	effective, _, _, _, err := resolveCatalogAgentRouting(ctx, c.catalogRoutingDeps())
	if err != nil {
		return "", err
	}
	agentDefault := configuredModel(c.agents, effective)

	if globalOnly {
		return c.showGlobal(ctx, effective, agentDefault)
	}

	sessionID, err := resolveCatalogSessionID(ctx, c.store)
	if err != nil {
		return "", err
	}
	if sessionID == "" {
		return "", fmt.Errorf("session id is required")
	}
	effectiveModel, sessionOverride, _, err := sessbind.ResolveEffectiveModel(
		ctx, c.store, c.workspace, sessionID, effective, agentDefault,
	)
	if err != nil {
		return "", err
	}
	globalOverride, err := sessbind.GlobalModelBind(ctx, c.workspace, effective)
	if err != nil {
		return "", err
	}

	var b strings.Builder
	if effectiveModel != "" {
		fmt.Fprintf(&b, "model: %s\n", effectiveModel)
	} else {
		b.WriteString("model: (not set)\n")
	}
	if sessionOverride != "" {
		fmt.Fprintf(&b, "session override: %s\n", strings.TrimSpace(sessionOverride))
	} else {
		b.WriteString("session override: (none)\n")
	}
	if globalOverride != "" {
		fmt.Fprintf(&b, "global override: %s\n", strings.TrimSpace(globalOverride))
	} else {
		b.WriteString("global override: (none)\n")
	}
	if agentDefault != "" {
		fmt.Fprintf(&b, "agent default: %s\n", agentDefault)
	}
	if ids := c.catalogIDs(); len(ids) > 0 {
		fmt.Fprintf(&b, "available: %s\n", strings.Join(ids, ", "))
	}
	b.WriteString("\nUse /model <name> for this session.")
	b.WriteString("\nUse /model -g <name> for all sessions (current agent).")
	b.WriteString("\nUse /model -g sub <name|*> <model> to configure subagents.")
	b.WriteString("\nUse /model reset or /model -g reset to clear.")
	return strings.TrimRight(b.String(), "\n"), nil
}

func (c modelCommand) showGlobal(ctx context.Context, agentID agentkit.AgentID, agentDefault string) (string, error) {
	if c.workspace == nil {
		return "", fmt.Errorf("workspace is not configured")
	}
	if agentID == "" {
		return "", fmt.Errorf("agent id is required")
	}
	globalOverride, err := sessbind.GlobalModelBind(ctx, c.workspace, agentID)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	if globalOverride != "" {
		fmt.Fprintf(&b, "global model (%s): %s\n", agentID, globalOverride)
	} else {
		fmt.Fprintf(&b, "global model (%s): (none)\n", agentID)
	}
	if agentDefault != "" {
		fmt.Fprintf(&b, "agent default: %s\n", agentDefault)
	}
	if ids := c.catalogIDs(); len(ids) > 0 {
		fmt.Fprintf(&b, "available: %s\n", strings.Join(ids, ", "))
	}
	b.WriteString("\nUse /model -g <name> to set the global default for this agent.")
	return strings.TrimRight(b.String(), "\n"), nil
}

func (c modelCommand) set(ctx context.Context, global bool, model string) (string, error) {
	model = strings.TrimSpace(model)
	if model == "" {
		if global {
			return "", fmt.Errorf("usage: /model -g <name>")
		}
		return "", fmt.Errorf("usage: /model <name>")
	}
	if global {
		return c.setGlobal(ctx, model)
	}
	sessionID, err := resolveCatalogSessionID(ctx, c.store)
	if err != nil {
		return "", err
	}
	if sessionID == "" {
		return "", fmt.Errorf("session id is required")
	}
	if err := sessbind.SetSessionModelBind(ctx, c.store, sessionID, model); err != nil {
		return "", err
	}
	return fmt.Sprintf("session model: %s%s", model, c.unknownModelWarning(model)), nil
}

func (c modelCommand) setGlobal(ctx context.Context, model string) (string, error) {
	agentID, _, _, _, err := resolveCatalogAgentRouting(ctx, c.catalogRoutingDeps())
	if err != nil {
		return "", err
	}
	if agentID == "" {
		return "", fmt.Errorf("agent id is required")
	}
	if err := sessbind.SetGlobalModelBind(ctx, c.workspace, agentID, model); err != nil {
		return "", err
	}
	return fmt.Sprintf("global model (%s): %s%s", agentID, model, c.unknownModelWarning(model)), nil
}

func (c modelCommand) clear(ctx context.Context, global bool) (string, error) {
	if global {
		agentID, _, _, _, err := resolveCatalogAgentRouting(ctx, c.catalogRoutingDeps())
		if err != nil {
			return "", err
		}
		if agentID == "" {
			return "", fmt.Errorf("agent id is required")
		}
		if err := sessbind.SetGlobalModelBind(ctx, c.workspace, agentID, ""); err != nil {
			return "", err
		}
		agentDefault := configuredModel(c.agents, agentID)
		if agentDefault != "" {
			return fmt.Sprintf("global model reset (%s); agent default: %s", agentID, agentDefault), nil
		}
		return fmt.Sprintf("global model reset (%s)", agentID), nil
	}
	sessionID, err := resolveCatalogSessionID(ctx, c.store)
	if err != nil {
		return "", err
	}
	if sessionID == "" {
		return "", fmt.Errorf("session id is required")
	}
	if err := sessbind.SetSessionModelBind(ctx, c.store, sessionID, ""); err != nil {
		return "", err
	}
	agentID, _, _, _, err := resolveCatalogAgentRouting(ctx, c.catalogRoutingDeps())
	if err != nil {
		return "", err
	}
	effective, _, globalOverride, err := sessbind.ResolveEffectiveModel(
		ctx, c.store, c.workspace, sessionID, agentID, configuredModel(c.agents, agentID),
	)
	if err != nil {
		return "", err
	}
	if globalOverride != "" {
		return fmt.Sprintf("session model reset; using global: %s", strings.TrimSpace(globalOverride)), nil
	}
	if effective != "" {
		return fmt.Sprintf("session model reset; using: %s", effective), nil
	}
	return "session model reset", nil
}
