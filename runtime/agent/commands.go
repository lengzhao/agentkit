package agent

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/session"
)

// Command exposes the agent catalog and session agent switching.
func Command(agents []agentkit.Agent, store agentkit.SessionStore, defaultAgent agentkit.AgentID) agentkit.Command {
	return agentCommand{agents: agents, store: store, defaultAgent: defaultAgent}
}

// HelpCommand exposes the agent catalog help slash command for built agent instances.
func HelpCommand(agents []agentkit.Agent) agentkit.Command {
	return Command(agents, nil, "")
}

type agentCommand struct {
	agents       []agentkit.Agent
	store        agentkit.SessionStore
	defaultAgent agentkit.AgentID
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
		return c.formatAgentList(ctx), nil
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
	sessionID := session.SessionIDFromContext(ctx)
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

func (c agentCommand) formatAgentList(ctx context.Context) string {
	ids := collectAgentIDs(c.agents)
	effective, bound, err := c.resolveEffectiveAgent(ctx)
	if err != nil {
		return fmt.Sprintf("agent list failed: %v", err)
	}

	var b strings.Builder
	writeAgentRoutingHeader(&b, effective, bound, c.defaultAgent)

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
	b.WriteString("\nUse /agent use <id> to switch this session.")
	return b.String()
}

func writeAgentRoutingHeader(b *strings.Builder, effective, bound, defaultAgent agentkit.AgentID) {
	effective = agentkit.AgentID(strings.TrimSpace(string(effective)))
	bound = agentkit.AgentID(strings.TrimSpace(string(bound)))
	defaultAgent = agentkit.AgentID(strings.TrimSpace(string(defaultAgent)))

	switch {
	case effective != "" && bound != "":
		fmt.Fprintf(b, "Session agent: %s\n", effective)
		if defaultAgent != "" && defaultAgent != effective {
			fmt.Fprintf(b, "Default agent: %s\n", defaultAgent)
		}
	case effective != "":
		if bound == "" && defaultAgent != "" && effective == defaultAgent {
			fmt.Fprintf(b, "Session agent: %s (default)\n", effective)
		} else {
			fmt.Fprintf(b, "Session agent: %s\n", effective)
		}
	case defaultAgent != "":
		fmt.Fprintf(b, "Default agent: %s\n", defaultAgent)
	}
	if b.Len() > 0 {
		b.WriteByte('\n')
	}
}

func (c agentCommand) resolveSessionID(ctx context.Context) (agentkit.SessionID, error) {
	sessionID := session.SessionIDFromContext(ctx)
	if sessionID == "" {
		return "", nil
	}
	if c.store == nil {
		return sessionID, nil
	}
	if activeStore, ok := c.store.(agentkit.ActiveSessionStore); ok {
		active, err := activeStore.ActiveSession(ctx, sessionID)
		if err != nil {
			return "", err
		}
		if active != "" {
			sessionID = active
		}
	}
	return sessionID, nil
}

func (c agentCommand) resolveEffectiveAgent(ctx context.Context) (effective, bound agentkit.AgentID, err error) {
	sessionID, err := c.resolveSessionID(ctx)
	if err != nil {
		return "", "", err
	}
	if sessionID != "" && c.store != nil {
		if bindStore, ok := c.store.(agentkit.AgentBindStore); ok {
			bound, err = bindStore.AgentBind(ctx, sessionID)
			if err != nil {
				return "", "", err
			}
		}
	}
	if bound != "" {
		return bound, bound, nil
	}
	return c.defaultAgent, "", nil
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
