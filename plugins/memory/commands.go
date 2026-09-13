package memory

import (
	"context"
	"fmt"
	"strings"

	"github.com/lengzhao/agentkit"
	capmemory "github.com/lengzhao/agentkit/cap/memory"
	rtmem "github.com/lengzhao/agentkit/runtime/memory"
)

func (s *Service) Commands() []agentkit.Command {
	return []agentkit.Command{memoryCommand{svc: s}}
}

type memoryCommand struct {
	svc *Service
}

func (memoryCommand) Name() string        { return "memory" }
func (memoryCommand) Alias() string       { return "" }
func (memoryCommand) Description() string { return "manage personal memory.md (add, staged review, policy)" }

func (c memoryCommand) CommandExec(ctx context.Context, args string) (string, error) {
	if c.svc.disabled {
		return "", fmt.Errorf("memory is disabled in preset config")
	}
	fields := strings.Fields(strings.TrimSpace(args))
	if len(fields) == 0 {
		return formatHelp(), nil
	}
	switch strings.ToLower(fields[0]) {
	case "help", "-h", "--help":
		return formatHelp(), nil
	case "show":
		return c.svc.show(ctx)
	case "add":
		text := strings.TrimSpace(strings.Join(fields[1:], " "))
		if text == "" {
			return "", fmt.Errorf("usage: /memory add <text>")
		}
		return c.svc.addMemory(ctx, text, "memory-cmd")
	case "remove", "rm":
		text := strings.TrimSpace(strings.Join(fields[1:], " "))
		if text == "" {
			return "", fmt.Errorf("usage: /memory remove <text>")
		}
		return c.svc.removeMemory(ctx, text, "memory-cmd")
	case "pending":
		return c.svc.listPending(ctx)
	case "approve":
		return c.svc.ApproveStaged(ctx, strings.TrimSpace(strings.Join(fields[1:], " ")))
	case "reject":
		return c.svc.RejectStaged(ctx, strings.TrimSpace(strings.Join(fields[1:], " ")))
	case "policy":
		return c.svc.handleMemoryPolicy(ctx, fields[1:])
	default:
		return "", fmt.Errorf("unknown /memory command %q (try /memory help)", fields[0])
	}
}

func (s *Service) show(ctx context.Context) (string, error) {
	entries, used, limit, err := s.LoadEntries(ctx)
	if err != nil {
		return "", err
	}
	if len(entries) > 0 {
		return FormatMemory(entries, used, limit), nil
	}
	staged, err := s.ListStaged(ctx)
	if err != nil {
		return "", err
	}
	if len(staged) > 0 {
		return fmt.Sprintf("no memory.md entries yet (%d staged awaiting /memory approve)", len(staged)), nil
	}
	return "no personal memory yet (memory.md empty; background review may stage — try /memory pending)", nil
}

func (s *Service) listPending(ctx context.Context) (string, error) {
	entries, err := s.ListStaged(ctx)
	if err != nil {
		return "", err
	}
	if len(entries) == 0 {
		return "no staged memory", nil
	}
	var b strings.Builder
	b.WriteString("staged memory:\n")
	for _, e := range entries {
		fmt.Fprintf(&b, "  %s [%s] %s\n", e.ID, e.Source, formatStagedEntryLine(e))
	}
	return strings.TrimRight(b.String(), "\n"), nil
}

func formatStagedEntryLine(e capmemory.StagedEntry) string {
	return rtmem.StagedPendingSummary(rtmem.StagedMemory{
		Action:  e.Action,
		OldText: e.OldText,
		Content: e.Content,
	})
}
