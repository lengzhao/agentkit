package loop

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/workspace"
	"github.com/lengzhao/agentkit/runtime/rctx"
	"github.com/lengzhao/agentkit/runtime/session/sessbind"
)

// Command exposes the agent catalog and session or global agent switching.
func AgentCommand(agents []agentkit.Agent, store agentkit.SessionStore, defaultAgent agentkit.AgentID, ws workspace.Service) agentkit.Command {
	return agentCommand{agents: agents, store: store, defaultAgent: defaultAgent, workspace: ws}
}

// HelpCommand exposes the agent catalog help slash command for built agent instances.
func AgentHelpCommand(agents []agentkit.Agent) agentkit.Command {
	return AgentCommand(agents, nil, "", nil)
}

type agentCommand struct {
	agents       []agentkit.Agent
	store        agentkit.SessionStore
	defaultAgent agentkit.AgentID
	workspace    workspace.Service
}

func (agentCommand) Name() string  { return "agent" }
func (agentCommand) Alias() string { return "" }
func (agentCommand) Description() string {
	return "list agents, switch session, channel (-c), or global (-g) default agent"
}

type agentBindScope int

const (
	agentBindSession agentBindScope = iota
	agentBindChannel
	agentBindGlobal
)

func (c agentCommand) CommandExec(ctx context.Context, args string) (string, error) {
	global, _, rest := parseCatalogSlashArgs(args)
	channel, rest := peelChannelFlag(rest)
	if global && channel {
		return "", fmt.Errorf("cannot combine -g and -c")
	}
	payload := strings.TrimSpace(strings.Join(rest, " "))
	scope := agentBindSession
	if global {
		scope = agentBindGlobal
	} else if channel {
		scope = agentBindChannel
	}

	if len(rest) >= 2 && rest[0] == "use" {
		return c.useAgent(ctx, scope, strings.TrimSpace(strings.Join(rest[1:], " ")))
	}
	if strings.EqualFold(payload, "reset") || strings.EqualFold(payload, "default") {
		return c.clearBind(ctx, scope)
	}
	if len(rest) == 0 || rest[0] == "-l" || rest[0] == "--list" {
		return c.formatAgentList(ctx), nil
	}
	return agentDoc(c.agents, payload)
}

func (c agentCommand) useAgent(ctx context.Context, scope agentBindScope, name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		switch scope {
		case agentBindGlobal:
			return "", fmt.Errorf("usage: /agent -g use <id>")
		case agentBindChannel:
			return "", fmt.Errorf("usage: /agent -c use <id>")
		default:
			return "", fmt.Errorf("usage: /agent use <id>")
		}
	}
	if _, err := agentDoc(c.agents, name); err != nil {
		return "", err
	}
	switch scope {
	case agentBindGlobal:
		if err := sessbind.SetGlobalAgentBind(ctx, c.workspace, agentkit.AgentID(name)); err != nil {
			return "", err
		}
		return fmt.Sprintf("global agent: %s", name), nil
	case agentBindChannel:
		entryKey := rctx.ChannelEntryKeyFromContext(ctx)
		if entryKey == "" {
			return "", fmt.Errorf("channel entry key is required")
		}
		if err := sessbind.SetChannelAgentBind(ctx, c.store, c.workspace, entryKey, agentkit.AgentID(name)); err != nil {
			return "", err
		}
		return fmt.Sprintf("channel agent: %s", name), nil
	default:
		sessionID, err := resolveCatalogSessionID(ctx, c.store)
		if err != nil {
			return "", err
		}
		if sessionID == "" {
			return "", fmt.Errorf("session id is required")
		}
		if err := sessbind.SetSessionAgentBind(ctx, c.store, sessionID, agentkit.AgentID(name)); err != nil {
			return "", err
		}
		return fmt.Sprintf("session agent: %s", name), nil
	}
}

func (c agentCommand) clearBind(ctx context.Context, scope agentBindScope) (string, error) {
	switch scope {
	case agentBindGlobal:
		if err := sessbind.SetGlobalAgentBind(ctx, c.workspace, ""); err != nil {
			return "", err
		}
		if c.defaultAgent != "" {
			return fmt.Sprintf("global agent reset; loop default: %s", c.defaultAgent), nil
		}
		return "global agent reset", nil
	case agentBindChannel:
		entryKey := rctx.ChannelEntryKeyFromContext(ctx)
		if entryKey == "" {
			return "", fmt.Errorf("channel entry key is required")
		}
		if err := sessbind.SetChannelAgentBind(ctx, c.store, c.workspace, entryKey, ""); err != nil {
			return "", err
		}
		_, _, _, globalBind, _, err := resolveCatalogAgentRouting(ctx, c.catalogRoutingDeps())
		if err != nil {
			return "", err
		}
		if globalBind != "" {
			return fmt.Sprintf("channel agent reset; using global: %s", globalBind), nil
		}
		if c.defaultAgent != "" {
			return fmt.Sprintf("channel agent reset; loop default: %s", c.defaultAgent), nil
		}
		return "channel agent reset", nil
	default:
		sessionID, err := resolveCatalogSessionID(ctx, c.store)
		if err != nil {
			return "", err
		}
		if sessionID == "" {
			return "", fmt.Errorf("session id is required")
		}
		if err := sessbind.SetSessionAgentBind(ctx, c.store, sessionID, ""); err != nil {
			return "", err
		}
		_, _, channelBind, globalBind, _, err := resolveCatalogAgentRouting(ctx, c.catalogRoutingDeps())
		if err != nil {
			return "", err
		}
		if channelBind != "" {
			return fmt.Sprintf("session agent reset; using channel: %s", channelBind), nil
		}
		if globalBind != "" {
			return fmt.Sprintf("session agent reset; using global: %s", globalBind), nil
		}
		if c.defaultAgent != "" {
			return fmt.Sprintf("session agent reset; loop default: %s", c.defaultAgent), nil
		}
		return "session agent reset", nil
	}
}

func (c agentCommand) formatAgentList(ctx context.Context) string {
	ids := collectAgentIDs(c.agents)
	effective, sessionBind, channelBind, globalBind, loopDefault, err := resolveCatalogAgentRouting(ctx, c.catalogRoutingDeps())
	if err != nil {
		return fmt.Sprintf("agent list failed: %v", err)
	}

	var b strings.Builder
	writeAgentRoutingHeader(&b, effective, sessionBind, channelBind, globalBind, loopDefault)

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
	b.WriteString("\nUse /agent -c use <id> for this channel (until session override).")
	b.WriteString("\nUse /agent -g use <id> for every channel (until session or channel override).")
	return b.String()
}

func writeAgentRoutingHeader(b *strings.Builder, effective, sessionBind, channelBind, globalBind, loopDefault agentkit.AgentID) {
	effective = agentkit.AgentID(strings.TrimSpace(string(effective)))
	sessionBind = agentkit.AgentID(strings.TrimSpace(string(sessionBind)))
	channelBind = agentkit.AgentID(strings.TrimSpace(string(channelBind)))
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
	if channelBind != "" {
		fmt.Fprintf(b, "Channel override: %s\n", channelBind)
	} else {
		b.WriteString("Channel override: (none)\n")
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
		doc := fmt.Sprintf("agent %q", name)
		if entry, ok := ag.(agentkit.AgentCatalogEntry); ok {
			doc = entry.AgentCatalogEntry()
		}
		return doc, nil
	}
	return "", fmt.Errorf("unknown agent %q (try /agent)", name)
}
