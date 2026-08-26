package tool_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/lengzhao/agentkit"
	capschedule "github.com/lengzhao/agentkit/cap/schedule"
	"github.com/lengzhao/agentkit/cap/workspace"
	pluginschedule "github.com/lengzhao/agentkit/plugins/schedule"
	"github.com/lengzhao/agentkit/plugins/tool"
)

func newScheduleTool(t *testing.T, cfg tool.ScheduleConfig) (agentkit.Tool, capschedule.Registry) {
	t.Helper()
	registry, err := pluginschedule.NewFile(pluginschedule.FileConfig{Path: "schedule.json"},
		pluginschedule.FileDeps{Workspace: workspace.Static(t.TempDir())})
	if err != nil {
		t.Fatal(err)
	}
	tl, err := tool.NewSchedule(cfg, tool.ScheduleDeps{Schedule: registry})
	if err != nil {
		t.Fatal(err)
	}
	return agentkit.First(tl), registry
}

func decodeSchedule(t *testing.T, raw string) tool.ScheduleOutput {
	t.Helper()
	var out tool.ScheduleOutput
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatalf("decode %q: %v", raw, err)
	}
	return out
}

func TestScheduleToolRequiresRegistry(t *testing.T) {
	t.Parallel()

	if _, err := tool.NewSchedule(tool.ScheduleConfig{}, tool.ScheduleDeps{}); err == nil {
		t.Fatal("expected an error without a schedule dependency")
	}
}

func TestScheduleAddListRemove(t *testing.T) {
	t.Parallel()

	tl, registry := newScheduleTool(t, tool.ScheduleConfig{})
	ctx := context.Background()

	out := decodeSchedule(t, callTool(t, ctx, tl, `{"op":"add","cron":"0 9 * * 1-5","prompt":"weekday standup notes","note":"asked by me"}`))
	if len(out.Jobs) != 1 {
		t.Fatalf("jobs = %+v, want 1", out.Jobs)
	}
	job := out.Jobs[0]
	if job.Source != capschedule.SourceAgent {
		t.Fatalf("source = %q, want agent", job.Source)
	}
	if job.NextRun == "" {
		t.Fatal("the tool should resolve nextRun so the agent need not parse cron")
	}
	if job.Note != "asked by me" {
		t.Fatalf("note = %q", job.Note)
	}

	// The registry is the shared one the firing platform reads.
	stored, err := registry.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(stored) != 1 || stored[0].Prompt != "weekday standup notes" {
		t.Fatalf("registry = %+v", stored)
	}

	listed := decodeSchedule(t, callTool(t, ctx, tl, `{"op":"list"}`))
	if len(listed.Jobs) != 1 {
		t.Fatalf("list = %+v", listed.Jobs)
	}

	removed := decodeSchedule(t, callTool(t, ctx, tl, `{"op":"remove","id":"`+job.ID+`"}`))
	if len(removed.Jobs) != 0 {
		t.Fatalf("after remove = %+v", removed.Jobs)
	}
	if !strings.Contains(removed.Instruction, "removed") {
		t.Fatalf("instruction = %q", removed.Instruction)
	}
}

func TestScheduleRejectsBadInput(t *testing.T) {
	t.Parallel()

	tl, _ := newScheduleTool(t, tool.ScheduleConfig{})
	ctx := context.Background()

	// The tool builder turns handler errors into text results, so assert on those.
	cases := map[string]string{
		`{"op":"add","prompt":"x"}`:                     "requires a cron",
		`{"op":"add","cron":"@daily"}`:                  "requires a prompt",
		`{"op":"add","cron":"nope","prompt":"x"}`:       "fields",
		`{"op":"add","cron":"99 * * * *","prompt":"x"}`: "out of range",
		`{"op":"remove"}`:                               "requires an id",
		`{"op":"remove","id":"ghost"}`:                  "no job with id",
		`{"op":"frobnicate"}`:                           "unknown op",
	}
	for input, want := range cases {
		if got := callTool(t, ctx, tl, input); !strings.Contains(got, want) {
			t.Errorf("%s = %q, want it to mention %q", input, got, want)
		}
	}
}

// TestScheduleEnforcesJobLimit keeps a confused agent from filling the calendar.
func TestScheduleEnforcesJobLimit(t *testing.T) {
	t.Parallel()

	tl, _ := newScheduleTool(t, tool.ScheduleConfig{MaxJobs: 2})
	ctx := context.Background()

	for i := 0; i < 2; i++ {
		out := callTool(t, ctx, tl, `{"op":"add","cron":"@daily","prompt":"job"}`)
		if strings.Contains(out, "limit") {
			t.Fatalf("add %d unexpectedly hit the limit: %s", i, out)
		}
	}
	if got := callTool(t, ctx, tl, `{"op":"add","cron":"@daily","prompt":"one too many"}`); !strings.Contains(got, "limit of 2") {
		t.Fatalf("third add = %q, want a limit error", got)
	}

	// A cron typo must report the cron problem, not the quota.
	if got := callTool(t, ctx, tl, `{"op":"add","cron":"bogus","prompt":"x"}`); strings.Contains(got, "limit") {
		t.Fatalf("invalid cron reported as a quota error: %q", got)
	}
}

// TestScheduleLimitCountsOnlyAgentJobs: config-declared jobs belong to the
// preset, so they must not consume the agent's quota.
func TestScheduleLimitCountsOnlyAgentJobs(t *testing.T) {
	t.Parallel()

	tl, registry := newScheduleTool(t, tool.ScheduleConfig{MaxJobs: 1})
	ctx := context.Background()

	if err := registry.SyncSource(ctx, capschedule.SourceConfig, []capschedule.Job{
		{ID: "task-1", Cron: "@daily", Prompt: "from config"},
		{ID: "task-2", Cron: "@hourly", Prompt: "also config"},
	}); err != nil {
		t.Fatal(err)
	}
	if got := callTool(t, ctx, tl, `{"op":"add","cron":"@daily","prompt":"agent job"}`); strings.Contains(got, "limit") {
		t.Fatalf("config jobs consumed the agent quota: %q", got)
	}
	if got := callTool(t, ctx, tl, `{"op":"add","cron":"@daily","prompt":"second agent job"}`); !strings.Contains(got, "limit of 1") {
		t.Fatalf("second agent job = %q, want a limit error", got)
	}
}

func TestScheduleListIsEmptyWithoutJobs(t *testing.T) {
	t.Parallel()

	tl, _ := newScheduleTool(t, tool.ScheduleConfig{})
	out := decodeSchedule(t, callTool(t, context.Background(), tl, `{"op":"list"}`))
	if len(out.Jobs) != 0 {
		t.Fatalf("jobs = %+v, want none", out.Jobs)
	}
}
