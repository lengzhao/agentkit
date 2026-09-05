package agent

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/session"
)

// ACPCommand exposes ACP session config control for agent/acp-remote agents.
func ACPCommand(agents []agentkit.Agent, store agentkit.SessionStore) agentkit.Command {
	return acpCommand{agents: collectACPAgents(agents), store: store}
}

type acpCommand struct {
	agents []agentkit.ACPCommandCapable
	store  agentkit.SessionStore
}

func (acpCommand) Name() string  { return "acp" }
func (acpCommand) Alias() string { return "" }
func (acpCommand) Description() string {
	return "list ACP remote agents, show session config, or set config via session/set_config_option"
}

func (c acpCommand) CommandExec(ctx context.Context, args string) (string, error) {
	agentID, configKey, configValue, mode, err := parseACPArgs(args)
	if err != nil {
		return "", err
	}
	switch mode {
	case acpParseList:
		return formatACPAgentList(c.agents), nil
	case acpParseShow:
		return c.showCatalog(ctx, agentID)
	case acpParseConfigList:
		return c.showConfig(ctx, agentID)
	case acpParseConfigSet:
		return c.setConfig(ctx, agentID, configKey, configValue)
	default:
		return "", fmt.Errorf("internal acp parse mode")
	}
}

type acpParseMode int

const (
	acpParseList acpParseMode = iota
	acpParseShow
	acpParseConfigList
	acpParseConfigSet
)

func parseACPArgs(args string) (agentID, configKey, configValue string, mode acpParseMode, err error) {
	args = strings.TrimSpace(args)
	if args == "" {
		return "", "", "", acpParseList, nil
	}
	fields := strings.Fields(args)
	if len(fields) == 0 {
		return "", "", "", acpParseList, nil
	}
	agentID = fields[0]
	rest := strings.TrimSpace(args[len(agentID):])
	if rest == "" {
		return agentID, "", "", acpParseShow, nil
	}
	if fields[1] != "config" {
		return "", "", "", 0, fmt.Errorf("usage: /acp\n       /acp <agent>\n       /acp <agent> config\n       /acp <agent> config <key> <value>")
	}
	configRest := strings.TrimSpace(rest[len("config"):])
	if configRest == "" {
		return agentID, "", "", acpParseConfigList, nil
	}
	configFields := strings.Fields(configRest)
	if len(configFields) < 2 {
		return "", "", "", 0, fmt.Errorf("usage: /acp <agent> config <key> <value>")
	}
	configKey = configFields[0]
	configValue = strings.TrimSpace(configRest[len(configKey):])
	return agentID, configKey, configValue, acpParseConfigSet, nil
}

func (c acpCommand) showCatalog(ctx context.Context, agentID string) (string, error) {
	ag, err := c.lookupACPAgent(agentID)
	if err != nil {
		return "", err
	}
	sessionID, err := c.resolveSessionID(ctx)
	if err != nil {
		return "", err
	}
	catalog, err := ag.ACPCommandCatalog(ctx, sessionID)
	if err != nil {
		return "", err
	}
	return formatACPCatalog(agentID, catalog), nil
}

func (c acpCommand) showConfig(ctx context.Context, agentID string) (string, error) {
	ag, err := c.lookupACPAgent(agentID)
	if err != nil {
		return "", err
	}
	sessionID, err := c.resolveSessionID(ctx)
	if err != nil {
		return "", err
	}
	catalog, err := ag.ACPCommandCatalog(ctx, sessionID)
	if err != nil {
		return "", err
	}
	return formatACPConfigList(agentID, catalog.ConfigOptions), nil
}

func (c acpCommand) setConfig(ctx context.Context, agentID, key, value string) (string, error) {
	ag, err := c.lookupACPAgent(agentID)
	if err != nil {
		return "", err
	}
	sessionID, err := c.resolveSessionID(ctx)
	if err != nil {
		return "", err
	}
	return ag.SetACPConfigOption(ctx, sessionID, key, value)
}

func (c acpCommand) lookupACPAgent(agentID string) (agentkit.ACPCommandCapable, error) {
	for _, ag := range c.agents {
		if ag != nil && string(ag.ID()) == agentID {
			return ag, nil
		}
	}
	return nil, fmt.Errorf("unknown acp agent %q (try /acp)", agentID)
}

func (c acpCommand) resolveSessionID(ctx context.Context) (agentkit.SessionID, error) {
	sessionID := session.SessionIDFromContext(ctx)
	if sessionID == "" {
		return "", fmt.Errorf("session id is required")
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

func collectACPAgents(agents []agentkit.Agent) []agentkit.ACPCommandCapable {
	out := make([]agentkit.ACPCommandCapable, 0)
	for _, ag := range agents {
		if ag == nil {
			continue
		}
		capable, ok := ag.(agentkit.ACPCommandCapable)
		if !ok {
			continue
		}
		out = append(out, capable)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].ID() < out[j].ID()
	})
	return out
}

func formatACPAgentList(agents []agentkit.ACPCommandCapable) string {
	if len(agents) == 0 {
		return "ACP remote agents:\n  (none)\n\nUse /acp <agent> for session config."
	}
	width := 0
	for _, ag := range agents {
		width = max(width, len(ag.ID()))
	}
	var b strings.Builder
	b.WriteString("ACP remote agents:\n")
	for _, ag := range agents {
		fmt.Fprintf(&b, "  %-*s\n", width, ag.ID())
	}
	b.WriteString("\nUse /acp <agent> for session config and native command catalog.")
	b.WriteString("\nUse /acp <agent> config <key> <value> to set config via ACP.")
	return b.String()
}

func formatACPCatalog(agentID string, catalog agentkit.ACPCommandCatalog) string {
	var b strings.Builder
	fmt.Fprintf(&b, "agent: %s (agent/acp-remote)\n", agentID)
	if len(catalog.AvailableCommands) > 0 {
		b.WriteString("\nNative commands (ACP spec: send as prompt while chatting with this agent):\n")
		for _, cmd := range catalog.AvailableCommands {
			name := strings.TrimSpace(cmd.Name)
			if name == "" {
				continue
			}
			if !strings.HasPrefix(name, "/") {
				name = "/" + name
			}
			if cmd.Description != "" {
				fmt.Fprintf(&b, "  %-16s %s\n", name, cmd.Description)
			} else {
				fmt.Fprintf(&b, "  %s\n", name)
			}
		}
	}
	if len(catalog.ConfigOptions) > 0 {
		b.WriteString("\nConfig options (set via /acp):\n")
		for _, opt := range catalog.ConfigOptions {
			line := fmt.Sprintf("  %s (%s) = %s", opt.Name, opt.ID, opt.CurrentValue)
			if opt.Category != "" {
				line += " [" + opt.Category + "]"
			}
			b.WriteString(line)
			b.WriteByte('\n')
		}
	}
	if len(catalog.AvailableCommands) == 0 && len(catalog.ConfigOptions) == 0 {
		b.WriteString("\n(no config options advertised yet; start a turn with this agent first)\n")
	}
	b.WriteString("\nUsage:\n")
	fmt.Fprintf(&b, "  /acp %s config\n", agentID)
	fmt.Fprintf(&b, "  /acp %s config model claude-sonnet-5\n", agentID)
	return strings.TrimRight(b.String(), "\n")
}

func formatACPConfigList(agentID string, options []agentkit.ACPConfigOptionInfo) string {
	var b strings.Builder
	fmt.Fprintf(&b, "agent: %s config options\n", agentID)
	if len(options) == 0 {
		b.WriteString("\n(no config options advertised yet)\n")
		b.WriteString("\nUsage:\n")
		fmt.Fprintf(&b, "  /acp %s config <key> <value>\n", agentID)
		return b.String()
	}
	for _, opt := range options {
		key := opt.ID
		if opt.Category != "" {
			fmt.Fprintf(&b, "\n%s (%s) [%s] = %s\n", opt.Name, key, opt.Category, opt.CurrentValue)
		} else {
			fmt.Fprintf(&b, "\n%s (%s) = %s\n", opt.Name, key, opt.CurrentValue)
		}
		if opt.Description != "" {
			fmt.Fprintf(&b, "  %s\n", opt.Description)
		}
		for _, item := range opt.Options {
			if item.Name != "" && item.Name != item.Value {
				fmt.Fprintf(&b, "  - %s (%s)\n", item.Value, item.Name)
			} else {
				fmt.Fprintf(&b, "  - %s\n", item.Value)
			}
		}
	}
	b.WriteString("\nUsage:\n")
	fmt.Fprintf(&b, "  /acp %s config <key> <value>\n", agentID)
	return strings.TrimRight(b.String(), "\n")
}
