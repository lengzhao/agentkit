package schedule

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/lengzhao/agentkit"
	capschedule "github.com/lengzhao/agentkit/cap/schedule"
	"github.com/lengzhao/agentkit/runtime/session"
)

type scheduleBundle struct {
	tool     agentkit.Tool
	registry capschedule.Registry
}

func (b *scheduleBundle) Name() string { return b.tool.Name() }

func (b *scheduleBundle) Description() string { return b.tool.Description() }

func (b *scheduleBundle) InputSchema() agentkit.JSONSchema { return b.tool.InputSchema() }

func (b *scheduleBundle) Call(ctx context.Context, input json.RawMessage) (string, error) {
	return b.tool.Call(ctx, input)
}

func (b *scheduleBundle) Commands() []agentkit.Command {
	return []agentkit.Command{cronSlashCommand{registry: b.registry}}
}

type cronSlashCommand struct {
	registry capschedule.Registry
}

func (cronSlashCommand) Name() string { return "cron" }

func (cronSlashCommand) Alias() string { return "" }

func (cronSlashCommand) Description() string {
	return "list or remove scheduled jobs for the current channel"
}

func (c cronSlashCommand) CommandExec(ctx context.Context, args string) (string, error) {
	args = strings.TrimSpace(args)
	switch {
	case args == "", args == "list":
		return formatCronList(ctx, c.registry, false)
	case strings.HasPrefix(args, "list "):
		includeFired := strings.TrimSpace(strings.TrimPrefix(args, "list")) == "all"
		return formatCronList(ctx, c.registry, includeFired)
	case strings.HasPrefix(args, "remove "), strings.HasPrefix(args, "rm "), strings.HasPrefix(args, "del "):
		id := strings.TrimSpace(strings.Fields(args)[1])
		if id == "" {
			return "", fmt.Errorf("usage: /cron remove <id>")
		}
		removed, err := c.registry.Remove(ctx, id)
		if err != nil {
			return "", err
		}
		if !removed {
			return "", fmt.Errorf("no job with id %q in this channel", id)
		}
		return fmt.Sprintf("removed %s", id), nil
	default:
		return "", fmt.Errorf("usage: /cron [list|list all|remove <id>]")
	}
}

func formatCronList(ctx context.Context, registry capschedule.Registry, includeFired bool) (string, error) {
	jobs, err := registry.List(ctx)
	if err != nil {
		return "", err
	}
	channel := session.WorkspaceFromContext(ctx)
	var b strings.Builder
	if channel != "" {
		fmt.Fprintf(&b, "channel: %s\n", channel)
	}
	pending := 0
	for _, job := range jobs {
		if job.Fired && !includeFired {
			continue
		}
		pending++
		writeCronLine(&b, job)
	}
	if pending == 0 {
		b.WriteString("no scheduled jobs")
	}
	return strings.TrimRight(b.String(), "\n"), nil
}

func writeCronLine(b *strings.Builder, job capschedule.Job) {
	kind := capschedule.JobKind(job)
	fmt.Fprintf(b, "- %s [%s]", job.ID, kind)
	switch kind {
	case capschedule.KindCron:
		if job.Cron != "" {
			fmt.Fprintf(b, " cron=%s", job.Cron)
		}
	case capschedule.KindDelay:
		if job.In != "" {
			fmt.Fprintf(b, " in=%s", job.In)
		}
	case capschedule.KindAt:
		if !job.FireAt.IsZero() {
			fmt.Fprintf(b, " at=%s", job.FireAt.Format(time.RFC3339))
		}
	}
	if next, ok := capschedule.NextFire(job, job.LastRun); ok && !job.Fired {
		fmt.Fprintf(b, " next=%s", next.Format(time.RFC3339))
	}
	if job.Fired {
		b.WriteString(" fired")
	}
	if job.Note != "" {
		fmt.Fprintf(b, " note=%q", job.Note)
	} else if job.Prompt != "" {
		fmt.Fprintf(b, " prompt=%q", truncate(job.Prompt, 40))
	}
	b.WriteByte('\n')
}

func truncate(text string, max int) string {
	runes := []rune(strings.TrimSpace(text))
	if len(runes) <= max {
		return string(runes)
	}
	return string(runes[:max]) + "…"
}
