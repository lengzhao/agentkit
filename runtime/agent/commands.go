package agent

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/lengzhao/agentkit"
)

// HelpCommand exposes the agent catalog help slash command for built agent instances.
func HelpCommand(agents []agentkit.Agent) agentkit.Command {
	return agentHelpCommand{agents: agents}
}

type agentHelpCommand struct {
	agents []agentkit.Agent
}

func (agentHelpCommand) Name() string        { return "agent" }
func (agentHelpCommand) Alias() string       { return "" }
func (agentHelpCommand) Description() string { return "list agents or show agent details" }

func (c agentHelpCommand) CommandExec(_ context.Context, args ...string) (string, error) {
	if len(args) == 0 || args[0] == "-l" || args[0] == "--list" {
		return formatAgentList(c.agents), nil
	}
	name := strings.TrimSpace(strings.Join(args, " "))
	return agentDoc(c.agents, name)
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
