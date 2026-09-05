package schedule_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/lengzhao/agentkit"
	capschedule "github.com/lengzhao/agentkit/cap/schedule"
	rtworkspace "github.com/lengzhao/agentkit/runtime/workspace"
	pluginschedule "github.com/lengzhao/agentkit/plugins/schedule"
	toolschedule "github.com/lengzhao/agentkit/plugins/tool/schedule"
	"github.com/lengzhao/agentkit/runtime/session"
)

func TestCronSlashListsChannelJobs(t *testing.T) {
	t.Parallel()

	reg, err := pluginschedule.NewMulti(pluginschedule.MultiConfig{Path: "schedule.json"}, pluginschedule.MultiDeps{
		Workspace: rtworkspace.Static(t.TempDir()),
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := session.ApplyEnvelopeToContext(context.Background(), agentkit.TurnEnvelope{Route: agentkit.SessionRoute("slack", "slack:C001"), Conversation: "slack:C001", Workspace: "slack:C001"})
	ctx = session.ContextWithDeliveryRoute(ctx, "slack", agentkit.SessionID("slack:C001:u:U1"))
	if _, err := reg.Add(ctx, capschedule.Job{
		Kind:   capschedule.KindCron,
		Cron:   "@daily",
		Prompt: "standup",
	}); err != nil {
		t.Fatal(err)
	}

	tool, err := toolschedule.NewSchedule(toolschedule.ScheduleConfig{}, toolschedule.ScheduleDeps{Schedule: reg})
	if err != nil {
		t.Fatal(err)
	}
	provider, ok := tool.(agentkit.CommandProvider)
	if !ok {
		t.Fatal("expected CommandProvider")
	}
	var cronCmd agentkit.Command
	for _, cmd := range provider.Commands() {
		if cmd.Name() == "cron" {
			cronCmd = cmd
			break
		}
	}
	if cronCmd == nil {
		t.Fatal("missing /cron command")
	}

	out, err := cronCmd.CommandExec(ctx, "list")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "agent-1") || !strings.Contains(out, "standup") {
		t.Fatalf("list = %q", out)
	}
	if !strings.Contains(out, "channel: slack:C001") {
		t.Fatalf("list = %q, want channel header", out)
	}
}

func TestCronSlashRemove(t *testing.T) {
	t.Parallel()

	reg, err := pluginschedule.NewMulti(pluginschedule.MultiConfig{Path: "schedule.json"}, pluginschedule.MultiDeps{
		Workspace: rtworkspace.Static(t.TempDir()),
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := session.ApplyEnvelopeToContext(context.Background(), agentkit.TurnEnvelope{Route: agentkit.SessionRoute("slack", "slack:C001"), Conversation: "slack:C001", Workspace: "slack:C001"})
	if _, err := reg.Add(ctx, capschedule.Job{
		Kind:   capschedule.KindDelay,
		FireAt: time.Now().Add(time.Minute),
		Prompt: "ping",
	}); err != nil {
		t.Fatal(err)
	}
	tool, err := toolschedule.NewSchedule(toolschedule.ScheduleConfig{}, toolschedule.ScheduleDeps{Schedule: reg})
	if err != nil {
		t.Fatal(err)
	}
	cmd := tool.(agentkit.CommandProvider).Commands()[0]
	if _, err := cmd.CommandExec(ctx, "remove agent-1"); err != nil {
		t.Fatal(err)
	}
	jobs, err := reg.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 0 {
		t.Fatalf("jobs = %+v", jobs)
	}
}
