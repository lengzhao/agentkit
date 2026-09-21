package workshop_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/lengzhao/agentkit/cap/filesystem"
	"github.com/lengzhao/agentkit/plugins/learning/workshop"
	rtfilesystem "github.com/lengzhao/agentkit/runtime/filesystem"
	rtworkspace "github.com/lengzhao/agentkit/runtime/workspace"
)

func testFS(t *testing.T, dir string) filesystem.Service {
	t.Helper()
	fs, err := rtfilesystem.New(rtfilesystem.Config{Root: "."}, rtfilesystem.Deps{Workspace: rtworkspace.Static(dir)})
	if err != nil {
		t.Fatal(err)
	}
	return fs
}

func TestWorkshopCreateApply(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	fs := testFS(t, root)
	ctx := context.Background()
	skillsDir := "skills"
	store := &workshop.Store{FS: fs, Root: "skills/.workshop"}
	body := workshop.DraftSkillBody("deploy-check", "Deployment checklist", "1. run tests\n2. deploy staging")
	p, err := store.Create(ctx, "deploy-check", body, "test", "cli:default", "", false)
	if err != nil {
		t.Fatal(err)
	}
	if p.Meta.Status != workshop.StatusPending {
		t.Fatalf("status=%s", p.Meta.Status)
	}
	if err := p.Apply(ctx, skillsDir); err != nil {
		t.Fatal(err)
	}
	skillPath := filepath.Join(root, "skills", "deploy-check", "SKILL.md")
	data, err := os.ReadFile(skillPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) == 0 {
		t.Fatal("expected SKILL.md content")
	}
}

func TestWorkshopCreateDoesNotWriteIntoExistingDirectory(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	skillsDir := filepath.Join(root, "skills")
	if err := os.MkdirAll(filepath.Join(skillsDir, "deploy-check"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillsDir, "deploy-check", "README.md"), []byte("handwritten"), 0o644); err != nil {
		t.Fatal(err)
	}
	store := &workshop.Store{FS: testFS(t, root), Root: "skills/.workshop"}
	body := workshop.DraftSkillBody("deploy-check", "Deployment checklist", "1. run tests")
	p, err := store.Create(context.Background(), "deploy-check", body, "test", "cli:default", "", false)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Apply(context.Background(), "skills"); err == nil {
		t.Fatal("expected apply to reject existing skill directory")
	}
}

func TestFormatListShowsEmptyWhenNoPendingProposals(t *testing.T) {
	t.Parallel()

	got := workshop.FormatList([]workshop.Proposal{{
		Meta: workshop.Meta{
			ID:        "p1",
			SkillName: "old-skill",
			Status:    workshop.StatusApplied,
		},
	}})
	if got != "no skill workshop proposals" {
		t.Fatalf("FormatList = %q", got)
	}
}

func TestScannerRejectsSecrets(t *testing.T) {
	t.Parallel()

	res := workshop.Scan("bad-skill", "api_key=secret-value")
	if res.OK {
		t.Fatal("expected scan failure")
	}
}
