package schedule_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/lengzhao/agentkit"
	capschedule "github.com/lengzhao/agentkit/cap/schedule"
	"github.com/lengzhao/agentkit/cap/workspace"
	pluginschedule "github.com/lengzhao/agentkit/plugins/schedule"
	"github.com/lengzhao/agentkit/plugins/tool/schedule"
	"github.com/lengzhao/agentkit/plugins/tool/testutil"
)

func newScheduleTool(t *testing.T, cfg schedule.ScheduleConfig) (agentkit.Tool, capschedule.Registry) {
	t.Helper()
	registry, err := pluginschedule.NewFile(pluginschedule.FileConfig{Path: "schedule.json"},
		pluginschedule.FileDeps{Workspace: workspace.Static(t.TempDir())})
	if err != nil {
		t.Fatal(err)
	}
	tl, err := schedule.NewSchedule(cfg, schedule.ScheduleDeps{Schedule: registry})
	if err != nil {
		t.Fatal(err)
	}
	return tl, registry
}

func decodeSchedule(t *testing.T, raw string) schedule.ScheduleOutput {
	t.Helper()
	var out schedule.ScheduleOutput
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatalf("decode %q: %v", raw, err)
	}
	return out
}

func TestScheduleToolRequiresRegistry(t *testing.T) {
	t.Parallel()

	if _, err := schedule.NewSchedule(schedule.ScheduleConfig{}, schedule.ScheduleDeps{}); err == nil {
		t.Fatal("expected an error without a schedule dependency")
	}
}

func TestScheduleAddListRemove(t *testing.T) {
	t.Parallel()

	tl, registry := newScheduleTool(t, schedule.ScheduleConfig{})
	ctx := context.Background()

	out := decodeSchedule(t, testutil.CallTool(t, ctx, tl, `{"op":"add","kind":"cron","cron":"0 9 * * 1-5","prompt":"weekday standup notes","note":"asked by me"}`))
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

	listed := decodeSchedule(t, testutil.CallTool(t, ctx, tl, `{"op":"list"}`))
	if len(listed.Jobs) != 1 {
		t.Fatalf("list = %+v", listed.Jobs)
	}

	removed := decodeSchedule(t, testutil.CallTool(t, ctx, tl, `{"op":"remove","id":"`+job.ID+`"}`))
	if len(removed.Jobs) != 0 {
		t.Fatalf("after remove = %+v", removed.Jobs)
	}
	if !strings.Contains(removed.Instruction, "removed") {
		t.Fatalf("instruction = %q", removed.Instruction)
	}
}

func TestScheduleRejectsBadInput(t *testing.T) {
	t.Parallel()

	tl, _ := newScheduleTool(t, schedule.ScheduleConfig{})
	ctx := context.Background()

	// The tool builder turns handler errors into text results, so assert on those.
	cases := map[string]string{
		`{"op":"add","prompt":"x"}`:                     "requires kind",
		`{"op":"add","kind":"cron"}`:                    "requires a prompt",
		`{"op":"add","kind":"cron","cron":"nope","prompt":"x"}`:       "fields",
		`{"op":"add","kind":"cron","cron":"99 * * * *","prompt":"x"}`: "out of range",
		`{"op":"add","kind":"delay","in":"1m","cron":"@daily","prompt":"x"}`: "only accepts in",
		`{"op":"remove"}`:                               "requires an id",
		`{"op":"remove","id":"ghost"}`:                  "no job with id",
		`{"op":"frobnicate"}`:                           "unknown op",
	}
	for input, want := range cases {
		if got := testutil.CallTool(t, ctx, tl, input); !strings.Contains(got, want) {
			t.Errorf("%s = %q, want it to mention %q", input, got, want)
		}
	}
}

// TestScheduleEnforcesJobLimit keeps a confused agent from filling the calendar.
func TestScheduleEnforcesJobLimit(t *testing.T) {
	t.Parallel()

	tl, _ := newScheduleTool(t, schedule.ScheduleConfig{MaxJobs: 2})
	ctx := context.Background()

	for i := 0; i < 2; i++ {
		out := testutil.CallTool(t, ctx, tl, `{"op":"add","kind":"cron","cron":"@daily","prompt":"job"}`)
		if strings.Contains(out, "limit") {
			t.Fatalf("add %d unexpectedly hit the limit: %s", i, out)
		}
	}
	if got := testutil.CallTool(t, ctx, tl, `{"op":"add","kind":"cron","cron":"@daily","prompt":"one too many"}`); !strings.Contains(got, "limit of 2") {
		t.Fatalf("third add = %q, want a limit error", got)
	}

	// A cron typo must report the cron problem, not the quota.
	if got := testutil.CallTool(t, ctx, tl, `{"op":"add","kind":"cron","cron":"bogus","prompt":"x"}`); strings.Contains(got, "limit") {
		t.Fatalf("invalid cron reported as a quota error: %q", got)
	}
}

// TestScheduleLimitCountsOnlyAgentJobs: config-declared jobs belong to the
// preset, so they must not consume the agent's quota.
func TestScheduleLimitCountsOnlyAgentJobs(t *testing.T) {
	t.Parallel()

	tl, registry := newScheduleTool(t, schedule.ScheduleConfig{MaxJobs: 1})
	ctx := context.Background()

	if err := registry.SyncSource(ctx, capschedule.SourceConfig, []capschedule.Job{
		{ID: "task-1", Cron: "@daily", Prompt: "from config"},
		{ID: "task-2", Cron: "@hourly", Prompt: "also config"},
	}); err != nil {
		t.Fatal(err)
	}
	if got := testutil.CallTool(t, ctx, tl, `{"op":"add","kind":"cron","cron":"@daily","prompt":"agent job"}`); strings.Contains(got, "limit") {
		t.Fatalf("config jobs consumed the agent quota: %q", got)
	}
	if got := testutil.CallTool(t, ctx, tl, `{"op":"add","kind":"cron","cron":"@daily","prompt":"second agent job"}`); !strings.Contains(got, "limit of 1") {
		t.Fatalf("second agent job = %q, want a limit error", got)
	}
}

func TestScheduleAddDelayKind(t *testing.T) {
	t.Parallel()

	tl, registry := newScheduleTool(t, schedule.ScheduleConfig{})
	ctx := context.Background()

	before := time.Now()
	out := decodeSchedule(t, testutil.CallTool(t, ctx, tl, `{"op":"add","kind":"delay","in":"2s","prompt":"ping","note":"test"}`))
	if len(out.Jobs) != 1 {
		t.Fatalf("jobs = %+v, want 1", out.Jobs)
	}
	job := out.Jobs[0]
	if job.Kind != capschedule.KindDelay {
		t.Fatalf("kind = %q, want delay", job.Kind)
	}
	if job.FireAt == "" {
		t.Fatal("expected fireAt in tool output")
	}

	stored, err := registry.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(stored) != 1 || stored[0].Kind != capschedule.KindDelay || stored[0].Cron != "" {
		t.Fatalf("stored = %+v, want delay job without cron", stored)
	}
	if stored[0].FireAt.Before(before.Add(1500*time.Millisecond)) || stored[0].FireAt.After(before.Add(3*time.Second)) {
		t.Fatalf("fireAt = %s, outside expected delay window", stored[0].FireAt)
	}
}

func TestScheduleCapturesDeliveryFromContext(t *testing.T) {
	t.Parallel()

	tl, registry := newScheduleTool(t, schedule.ScheduleConfig{})
	ctx := context.WithValue(context.Background(), agentkit.KeyDeliverySessionID, agentkit.SessionID("chat-api:ch:t:conv"))
	ctx = context.WithValue(ctx, agentkit.KeyPlatformID, "chat-api")
	ctx = context.WithValue(ctx, agentkit.KeyUserID, "u1")
	ctx = context.WithValue(ctx, agentkit.KeyAgentID, agentkit.AgentID("assistant"))

	testutil.CallTool(t, ctx, tl, `{"op":"add","kind":"delay","in":"1m","prompt":"remind"}`)

	stored, err := registry.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(stored) != 1 {
		t.Fatalf("jobs = %+v", stored)
	}
	job := stored[0]
	if job.DeliverySessionID != "chat-api:ch:t:conv" {
		t.Fatalf("delivery = %q", job.DeliverySessionID)
	}
	if job.PlatformID != "chat-api" || job.UserID != "u1" || job.AgentID != "assistant" {
		t.Fatalf("routing = %+v", job)
	}
	if job.Kind != capschedule.KindDelay || job.FireAt.IsZero() {
		t.Fatalf("delay job missing fireAt, got %+v", job)
	}
}

func TestScheduleListIsEmptyWithoutJobs(t *testing.T) {
	t.Parallel()

	tl, _ := newScheduleTool(t, schedule.ScheduleConfig{})
	out := decodeSchedule(t, testutil.CallTool(t, context.Background(), tl, `{"op":"list"}`))
	if len(out.Jobs) != 0 {
		t.Fatalf("jobs = %+v, want none", out.Jobs)
	}
}
