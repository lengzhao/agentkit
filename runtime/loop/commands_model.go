package loop

import (
	"context"
	"fmt"
	"strings"

	"github.com/lengzhao/agentkit"
	capllm "github.com/lengzhao/agentkit/cap/llm"
	"github.com/lengzhao/agentkit/cap/workspace"
	"github.com/lengzhao/agentkit/runtime/session/sessbind"
)

// ModelCommand shows or sets the session or global LLM model override for coding agents.
//
// The catalog is optional: when nil the command still works, just without the
// available-model list and unknown-model warnings. Commands for other domains
// (failover chain, subagent models) are provided by their own plugins.
func ModelCommand(agents []agentkit.Agent, store agentkit.SessionStore, defaultAgent agentkit.AgentID, ws workspace.Service, catalog capllm.ModelCatalog) agentkit.Command {
	return modelCommand{agents: agents, store: store, defaultAgent: defaultAgent, workspace: ws, catalog: catalog}
}

type modelCommand struct {
	agents       []agentkit.Agent
	store        agentkit.SessionStore
	defaultAgent agentkit.AgentID
	workspace    workspace.Service
	catalog      capllm.ModelCatalog
}

func (modelCommand) Name() string  { return "model" }
func (modelCommand) Alias() string { return "" }
func (modelCommand) Description() string {
	return "show or set the LLM model for this session (-g for global default)"
}

func (c modelCommand) CommandExec(ctx context.Context, args string) (string, error) {
	global, payload, _ := parseCatalogSlashArgs(args)

	if hint := relocatedScopeHint(payload); hint != "" {
		return "", fmt.Errorf("%s", hint)
	}

	if payload == "" || isModelShowPayload(payload) {
		return c.show(ctx, global)
	}
	if strings.EqualFold(payload, "reset") || strings.EqualFold(payload, "default") {
		return c.clear(ctx, global)
	}
	return c.set(ctx, global, payload)
}

// relocatedScopeHint points scopes that moved to their own plugins at their new
// command, so a stale habit fails loudly instead of writing a bogus model bind.
func relocatedScopeHint(payload string) string {
	fields := strings.Fields(strings.TrimSpace(payload))
	if len(fields) == 0 {
		return ""
	}
	head := strings.ToLower(fields[0])
	if head == "fallback" || head == "fb" {
		return "the fallback chain view moved: use /fallback (provided by the llm/fallback plugin)"
	}
	if head == "sub" || strings.HasPrefix(head, "sub:") {
		return "subagent model config moved: use /submodel (provided by the subagent/inprocess plugin)"
	}
	return ""
}

// isModelShowPayload treats common "display status" typos as bare /model.
func isModelShowPayload(payload string) bool {
	switch strings.ToLower(strings.TrimSpace(payload)) {
	case "show", "status", "list":
		return true
	default:
		return false
	}
}

// catalogIDs lists catalog model ids when a capllm.ModelCatalog is wired.
func (c modelCommand) catalogIDs() []string {
	if c.catalog == nil {
		return nil
	}
	entries := c.catalog.CatalogModels()
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
	effective, _, _, _, _, err := resolveCatalogAgentRouting(ctx, c.catalogRoutingDeps())
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
	agentID, _, _, _, _, err := resolveCatalogAgentRouting(ctx, c.catalogRoutingDeps())
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
		agentID, _, _, _, _, err := resolveCatalogAgentRouting(ctx, c.catalogRoutingDeps())
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
	agentID, _, _, _, _, err := resolveCatalogAgentRouting(ctx, c.catalogRoutingDeps())
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
