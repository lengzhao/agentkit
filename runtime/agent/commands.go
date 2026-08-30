package agent

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/lengzhao/agentkit"
)

// Command exposes the agent catalog and session agent switching.
func Command(agents []agentkit.Agent, store agentkit.SessionStore) agentkit.Command {
	return agentCommand{agents: agents, store: store}
}

// HelpCommand exposes the agent catalog help slash command for built agent instances.
func HelpCommand(agents []agentkit.Agent) agentkit.Command {
	return Command(agents, nil)
}

type agentCommand struct {
	agents []agentkit.Agent
	store  agentkit.SessionStore
}

func (agentCommand) Name() string        { return "agent" }
func (agentCommand) Alias() string       { return "" }
func (agentCommand) Description() string { return "list agents, show details, or switch session agent" }

func (c agentCommand) CommandExec(ctx context.Context, args string) (string, error) {
	fields := strings.Fields(strings.TrimSpace(args))
	if len(fields) >= 2 && fields[0] == "use" {
		return c.useAgent(ctx, strings.TrimSpace(strings.Join(fields[1:], " ")))
	}
	if len(fields) == 0 || fields[0] == "-l" || fields[0] == "--list" {
		return formatAgentList(c.agents), nil
	}
	return agentDoc(c.agents, strings.TrimSpace(args))
}

func (c agentCommand) useAgent(ctx context.Context, name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", fmt.Errorf("usage: /agent use <id>")
	}
	if _, err := agentDoc(c.agents, name); err != nil {
		return "", err
	}
	sessionID, _ := ctx.Value(agentkit.KeySessionID).(agentkit.SessionID)
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
	if activeStore, ok := c.store.(agentkit.ActiveSessionStore); ok {
		active, err := activeStore.ActiveSession(ctx, sessionID)
		if err != nil {
			return "", err
		}
		sessionID = active
	}
	if err := bindStore.SetAgentBind(ctx, sessionID, agentkit.AgentID(name)); err != nil {
		return "", err
	}
	return fmt.Sprintf("session agent: %s", name), nil
}

func formatAgentList(agents []agentkit.Agent) string {
	ids := collectAgentIDs(agents)
	if len(ids) == 0 {
		return "Registered agents:\n  (none)\n\nUse /agent <id> for details."
	}
	width := 0
	for _, id := range ids {
		width = max(width, len(id))
	}
	var b strings.Builder
	b.WriteString("Registered agents:\n")
	for _, id := range ids {
		fmt.Fprintf(&b, "  %-*s\n", width, id)
	}
	b.WriteString("\nUse /agent <id> for details.")
	b.WriteString("\nUse /agent use <id> to switch this session.")
	return b.String()
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
