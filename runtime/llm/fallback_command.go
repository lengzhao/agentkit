package llm

import (
	"context"
	"fmt"
	"strings"

	"github.com/lengzhao/agentkit"
)

// Commands implements agentkit.CommandProvider: llm/fallback contributes the
// /fallback slash command itself, so commands/registry collects it through the
// generic WireContributions channel with no bespoke wiring in other plugins.
func (f *Fallback) Commands() []agentkit.Command {
	return []agentkit.Command{fallbackCommand{fallback: f}}
}

// fallbackCommand is a read-only view of the llm/fallback failover chain.
// The chain is process-wide config, so the command needs no session, workspace
// or loop deps — and must not take any: llm/fallback sits beneath the agents
// that depend on it, and routing deps would close a build cycle.
type fallbackCommand struct {
	fallback *Fallback
}

func (fallbackCommand) Name() string  { return "fallback" }
func (fallbackCommand) Alias() string { return "fb" }
func (fallbackCommand) Description() string {
	return "show the llm/fallback failover chain (read-only)"
}

func (c fallbackCommand) CommandExec(_ context.Context, args string) (string, error) {
	fields := strings.Fields(strings.TrimSpace(args))
	primary := ""
	switch len(fields) {
	case 0:
	case 1:
		switch strings.ToLower(fields[0]) {
		case "show", "status", "list":
		default:
			primary = fields[0]
		}
	default:
		return "", fmt.Errorf("usage: /fallback [model] (read-only; edit config llm.fallback)")
	}

	f := c.fallback
	var b strings.Builder
	fmt.Fprintf(&b, "fallback on: %s\n", f.FallbackOn())
	if names := providerNames(f.providers); len(names) > 1 {
		fmt.Fprintf(&b, "providers: %s\n", strings.Join(names, " → "))
	}
	if f.UsesRequestModel() {
		cfg := f.ConfiguredModels()
		if len(cfg) == 0 {
			b.WriteString("fallback models: (none configured)\n")
		} else {
			fmt.Fprintf(&b, "fallback models: %s\n", strings.Join(cfg, ", "))
		}
		if primary == "" {
			primary = "<request model>"
		} else {
			fmt.Fprintf(&b, "primary model: %s\n", primary)
		}
		fmt.Fprintf(&b, "attempt chain (primary + fallback): %s\n", formatModelChain(f.ChainFor(primary)))
	} else {
		fmt.Fprintf(&b, "fixed model chain: %s\n", formatModelChain(f.ChainFor(primary)))
	}
	b.WriteString("\nFailover list is defined in llm/fallback config (restart to apply yaml changes).")
	return strings.TrimRight(b.String(), "\n"), nil
}

func formatModelChain(chain []string) string {
	if len(chain) == 0 {
		return "(empty)"
	}
	return strings.Join(chain, " → ")
}

func providerNames(providers []agentkit.LLMProvider) []string {
	out := make([]string, 0, len(providers))
	for _, p := range providers {
		if p == nil {
			continue
		}
		out = append(out, p.Name())
	}
	return out
}
