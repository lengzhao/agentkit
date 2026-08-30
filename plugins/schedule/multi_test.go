package schedule_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/lengzhao/agentkit"
	capschedule "github.com/lengzhao/agentkit/cap/schedule"
	"github.com/lengzhao/agentkit/cap/workspace"
	"github.com/lengzhao/agentkit/plugins/schedule"
	workspaceplugin "github.com/lengzhao/agentkit/runtime/workspace"
)

func newMultiRegistry(t *testing.T) (capschedule.Registry, string) {
	t.Helper()
	globalRoot := t.TempDir()
	ws, err := workspaceplugin.NewTenant(workspaceplugin.TenantConfig{
		Global: globalRoot,
		Scope:  workspace.ScopeGlobal,
	})
	if err != nil {
		t.Fatal(err)
	}
	reg, err := schedule.NewMulti(schedule.MultiConfig{}, schedule.MultiDeps{Workspace: ws})
	if err != nil {
		t.Fatal(err)
	}
	return reg, globalRoot
}

func TestMultiIsolatesJobsPerChannel(t *testing.T) {
	t.Parallel()

	reg, globalRoot := newMultiRegistry(t)
	ch1 := context.WithValue(context.Background(), agentkit.KeySessionID, agentkit.SessionID("slack:C001"))
	ch1 = context.WithValue(ch1, agentkit.KeyDeliverySessionID, agentkit.SessionID("slack:C001:u:U1"))
	ch2 := context.WithValue(context.Background(), agentkit.KeySessionID, agentkit.SessionID("slack:C002"))
	ch2 = context.WithValue(ch2, agentkit.KeyDeliverySessionID, agentkit.SessionID("slack:C002:u:U2"))

	if _, err := reg.Add(ch1, capschedule.Job{
		Kind:   capschedule.KindDelay,
		In:     "1m",
		FireAt: time.Now().Add(time.Minute),
		Prompt: "channel one",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := reg.Add(ch2, capschedule.Job{
		Kind:   capschedule.KindDelay,
		In:     "1m",
		FireAt: time.Now().Add(time.Minute),
		Prompt: "channel two",
	}); err != nil {
		t.Fatal(err)
	}

	list1, err := reg.List(ch1)
	if err != nil {
		t.Fatal(err)
	}
	if len(list1) != 1 || list1[0].Prompt != "channel one" {
		t.Fatalf("channel1 = %+v", list1)
	}
	list2, err := reg.List(ch2)
	if err != nil {
		t.Fatal(err)
	}
	if len(list2) != 1 || list2[0].Prompt != "channel two" {
		t.Fatalf("channel2 = %+v", list2)
	}

	schedulePath := filepath.Join(globalRoot, "schedules", "schedule.json")
	if _, err := os.Stat(schedulePath); err != nil {
		t.Fatalf("shared schedule file: %v", err)
	}

	all, err := reg.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Fatalf("aggregate list = %+v, want 2", all)
	}
}

func TestMultiRemoveRespectsChannelScope(t *testing.T) {
	t.Parallel()

	reg, _ := newMultiRegistry(t)
	ch1 := context.WithValue(context.Background(), agentkit.KeySessionID, agentkit.SessionID("slack:C001"))
	ch2 := context.WithValue(context.Background(), agentkit.KeySessionID, agentkit.SessionID("slack:C002"))

	added, err := reg.Add(ch1, capschedule.Job{
		Kind:   capschedule.KindDelay,
		FireAt: time.Now().Add(time.Minute),
		Prompt: "only c001",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reg.Add(ch2, capschedule.Job{
		Kind:   capschedule.KindDelay,
		FireAt: time.Now().Add(time.Minute),
		Prompt: "only c002",
	}); err != nil {
		t.Fatal(err)
	}

	removed, err := reg.Remove(ch2, added.ID)
	if err != nil {
		t.Fatal(err)
	}
	if removed {
		t.Fatal("channel2 should not remove channel1 job")
	}
	list1, err := reg.List(ch1)
	if err != nil {
		t.Fatal(err)
	}
	if len(list1) != 1 {
		t.Fatalf("channel1 jobs = %+v", list1)
	}
}

func TestMultiDueScansAllChannels(t *testing.T) {
	t.Parallel()

	reg, _ := newMultiRegistry(t)
	ch1 := context.WithValue(context.Background(), agentkit.KeySessionID, agentkit.SessionID("slack:C001"))
	now := time.Now()
	if _, err := reg.Add(ch1, capschedule.Job{
		Kind:   capschedule.KindDelay,
		FireAt: now.Add(-time.Second),
		Prompt: "due soon",
	}); err != nil {
		t.Fatal(err)
	}
	due, err := reg.Due(context.Background(), now)
	if err != nil {
		t.Fatal(err)
	}
	if len(due) != 1 || due[0].Prompt != "due soon" {
		t.Fatalf("due = %+v", due)
	}
}

func TestMultiSyncSourceStoresConfigJobs(t *testing.T) {
	t.Parallel()

	reg, _ := newMultiRegistry(t)
	ctx := context.Background()
	if err := reg.SyncSource(ctx, capschedule.SourceConfig, []capschedule.Job{{
		ID:     "job-1",
		Kind:   capschedule.KindCron,
		Cron:   "0 9 * * *",
		Prompt: "morning",
	}}); err != nil {
		t.Fatal(err)
	}
	jobs, err := reg.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 1 || jobs[0].ID != "job-1" {
		t.Fatalf("jobs = %+v", jobs)
	}
}
