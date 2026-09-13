package agent

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/workspace"
	"github.com/lengzhao/agentkit/runtime/configfile"
	"github.com/lengzhao/agentkit/runtime/session"
)

// Command exposes the agent catalog and session or global agent switching.
func Command(agents []agentkit.Agent, store agentkit.SessionStore, defaultAgent agentkit.AgentID, ws workspace.Service) agentkit.Command {
	return agentCommand{agents: agents, store: store, defaultAgent: defaultAgent, workspace: ws}
}

// HelpCommand exposes the agent catalog help slash command for built agent instances.
func HelpCommand(agents []agentkit.Agent) agentkit.Command {
	return Command(agents, nil, "", nil)
}

type agentCommand struct {
	agents       []agentkit.Agent
	store        agentkit.SessionStore
	defaultAgent agentkit.AgentID
	workspace    workspace.Service
}

func (agentCommand) Name() string        { return "agent" }
func (agentCommand) Alias() string       { return "" }
func (agentCommand) Description() string { return "list agents, switch session or global (-g) default agent" }

func (c agentCommand) CommandExec(ctx context.Context, args string) (string, error) {
	fields := strings.Fields(strings.TrimSpace(args))
	global, rest := configfile.PeelGlobalFlag(fields)
	payload := strings.TrimSpace(strings.Join(rest, " "))

	if len(rest) >= 2 && rest[0] == "use" {
		return c.useAgent(ctx, global, strings.TrimSpace(strings.Join(rest[1:], " ")))
	}
	if strings.EqualFold(payload, "reset") || strings.EqualFold(payload, "default") {
		return c.clearBind(ctx, global)
	}
	if len(rest) == 0 || rest[0] == "-l" || rest[0] == "--list" {
		return c.formatAgentList(ctx), nil
	}
	return agentDoc(c.agents, payload)
}

func (c agentCommand) useAgent(ctx context.Context, global bool, name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		if global {
			return "", fmt.Errorf("usage: /agent -g use <id>")
		}
		return "", fmt.Errorf("usage: /agent use <id>")
	}
	if _, err := agentDoc(c.agents, name); err != nil {
		return "", err
	}
	if global {
		if err := session.SetGlobalAgentBind(ctx, c.workspace, agentkit.AgentID(name)); err != nil {
			return "", err
		}
		return fmt.Sprintf("global agent: %s", name), nil
	}
	sessionID, err := resolveCatalogSessionID(ctx, c.store)
	if err != nil {
		return "", err
	}
	if sessionID == "" {
		return "", fmt.Errorf("session id is required")
	}
	if c.store == nil {
		return "", fmt.Errorf("session store is not configured")
	}
	bindStore, ok := c.store.(agentkit.AgentBindStore)
	if !ok {
		return "", fmt.Errorf("session store does not support agent binding")
	}
	if err := bindStore.SetAgentBind(ctx, sessionID, agentkit.AgentID(name)); err != nil {
		return "", err
	}
	return fmt.Sprintf("session agent: %s", name), nil
}

func (c agentCommand) clearBind(ctx context.Context, global bool) (string, error) {
	if global {
		if err := session.SetGlobalAgentBind(ctx, c.workspace, ""); err != nil {
			return "", err
		}
		if c.defaultAgent != "" {
			return fmt.Sprintf("global agent reset; loop default: %s", c.defaultAgent), nil
		}
		return "global agent reset", nil
	}
	sessionID, err := resolveCatalogSessionID(ctx, c.store)
	if err != nil {
		return "", err
	}
	if sessionID == "" {
		return "", fmt.Errorf("session id is required")
	}
	bindStore, ok := c.store.(agentkit.AgentBindStore)
	if !ok {
		return "", fmt.Errorf("session store does not support agent binding")
	}
	if err := bindStore.SetAgentBind(ctx, sessionID, ""); err != nil {
		return "", err
	}
	effective, _, globalBind, _, err := resolveCatalogAgentRouting(ctx, c.catalogRoutingDeps())
	if err != nil {
		return "", err
	}
	if globalBind != "" {
		return fmt.Sprintf("session agent reset; using global: %s", globalBind), nil
	}
	if effective != "" {
		return fmt.Sprintf("session agent reset; using: %s", effective), nil
	}
	return "session agent reset", nil
}

func (c agentCommand) formatAgentList(ctx context.Context) string {
	ids := collectAgentIDs(c.agents)
	effective, sessionBind, globalBind, loopDefault, err := resolveCatalogAgentRouting(ctx, c.catalogRoutingDeps())
	if err != nil {
		return fmt.Sprintf("agent list failed: %v", err)
	}

	var b strings.Builder
	writeAgentRoutingHeader(&b, effective, sessionBind, globalBind, loopDefault)

	if len(ids) == 0 {
		b.WriteString("Registered agents:\n  (none)\n\nUse /agent <id> for details.")
		return b.String()
	}
	width := 0
	for _, id := range ids {
		width = max(width, len(id))
	}
	b.WriteString("Registered agents:\n")
	current := strings.TrimSpace(string(effective))
	for _, id := range ids {
		marker := ""
		if current != "" && id == current {
			marker = " *"
		}
		fmt.Fprintf(&b, "  %-*s%s\n", width, id, marker)
	}
	b.WriteString("\nUse /agent <id> for details.")
	b.WriteString("\nUse /agent use <id> for this session.")
	b.WriteString("\nUse /agent -g use <id> for all sessions (until session override).")
	return b.String()
}

func writeAgentRoutingHeader(b *strings.Builder, effective, sessionBind, globalBind, loopDefault agentkit.AgentID) {
	effective = agentkit.AgentID(strings.TrimSpace(string(effective)))
	sessionBind = agentkit.AgentID(strings.TrimSpace(string(sessionBind)))
	globalBind = agentkit.AgentID(strings.TrimSpace(string(globalBind)))
	loopDefault = agentkit.AgentID(strings.TrimSpace(string(loopDefault)))

	if effective != "" {
		fmt.Fprintf(b, "Active agent: %s\n", effective)
	}
	if sessionBind != "" {
		fmt.Fprintf(b, "Session override: %s\n", sessionBind)
	} else {
		b.WriteString("Session override: (none)\n")
	}
	if globalBind != "" {
		fmt.Fprintf(b, "Global override: %s\n", globalBind)
	} else {
		b.WriteString("Global override: (none)\n")
	}
	if loopDefault != "" {
		fmt.Fprintf(b, "Loop default: %s\n", loopDefault)
	}
	if b.Len() > 0 {
		b.WriteByte('\n')
	}
}

func (c agentCommand) catalogRoutingDeps() catalogRoutingDeps {
	return catalogRoutingDeps{
		store:        c.store,
		defaultAgent: c.defaultAgent,
		workspace:    c.workspace,
	}
}

func collectAgentIDs(agents []agentkit.Agent) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(agents))
	for _, ag := range agents {
		if ag == nil {
			continue
		}
		id := strings.TrimSpace(string(ag.ID()))
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

func agentDoc(agents []agentkit.Agent, name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", fmt.Errorf("agent id is required")
	}
	for _, ag := range agents {
		if ag == nil {
			continue
		}
		if string(ag.ID()) != name {
			continue
		}
		if entry, ok := ag.(agentkit.AgentCatalogEntry); ok {
			return entry.AgentCatalogEntry(), nil
		}
		return fmt.Sprintf("agent %q", name), nil
	}
	return "", fmt.Errorf("unknown agent %q (try /agent)", name)
}

func (a *Runtime) AgentCatalogEntry() string {
	var b strings.Builder
	fmt.Fprintf(&b, "agent %q\n", a.id)
	b.WriteString("kind: agent/coding\n")
	if a.model != "" {
		fmt.Fprintf(&b, "model: %s\n", a.model)
	}
	if a.maxSteps > 0 {
		fmt.Fprintf(&b, "maxSteps: %d\n", a.maxSteps)
	}
	return strings.TrimRight(b.String(), "\n")
}
