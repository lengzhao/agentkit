package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/workspace"
	"github.com/lengzhao/agentkit/runtime/session"
)

// ModelCommand shows or sets the session or global LLM model override for coding agents.
func ModelCommand(agents []agentkit.Agent, store agentkit.SessionStore, defaultAgent agentkit.AgentID, ws workspace.Service) agentkit.Command {
	return modelCommand{agents: agents, store: store, defaultAgent: defaultAgent, workspace: ws}
}

type modelCommand struct {
	agents       []agentkit.Agent
	store        agentkit.SessionStore
	defaultAgent agentkit.AgentID
	workspace    workspace.Service
}

func (modelCommand) Name() string        { return "model" }
func (modelCommand) Alias() string       { return "" }
func (modelCommand) Description() string { return "show or set the LLM model for this session (-g for global default)" }

func (c modelCommand) CommandExec(ctx context.Context, args string) (string, error) {
	global, payload, _ := parseCatalogSlashArgs(args)

	if payload == "" {
		return c.show(ctx, global)
	}
	if strings.EqualFold(payload, "reset") || strings.EqualFold(payload, "default") {
		return c.clear(ctx, global)
	}
	return c.set(ctx, global, payload)
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
	effectiveModel, sessionOverride, globalOverride, err := session.ResolveEffectiveModel(
		ctx, c.store, c.workspace, sessionID, effective, agentDefault,
	)
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
	globalOverride, err := session.GlobalModelBind(ctx, c.workspace, agentID)
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
	if err := session.SetSessionModelBind(ctx, c.store, sessionID, model); err != nil {
		return "", err
	}
	return fmt.Sprintf("session model: %s", model), nil
}

func (c modelCommand) setGlobal(ctx context.Context, model string) (string, error) {
	agentID, _, _, _, err := resolveCatalogAgentRouting(ctx, c.catalogRoutingDeps())
	if err != nil {
		return "", err
	}
	if agentID == "" {
		return "", fmt.Errorf("agent id is required")
	}
	if err := session.SetGlobalModelBind(ctx, c.workspace, agentID, model); err != nil {
		return "", err
	}
	return fmt.Sprintf("global model (%s): %s", agentID, model), nil
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
		if err := session.SetGlobalModelBind(ctx, c.workspace, agentID, ""); err != nil {
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
	if err := session.SetSessionModelBind(ctx, c.store, sessionID, ""); err != nil {
		return "", err
	}
	agentID, _, _, _, err := resolveCatalogAgentRouting(ctx, c.catalogRoutingDeps())
	if err != nil {
		return "", err
	}
	effective, _, globalOverride, err := session.ResolveEffectiveModel(
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
