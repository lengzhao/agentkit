package workshop_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/lengzhao/agentkit/plugins/learning/workshop"
)

func TestWorkshopCreateApply(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	skillsDir := filepath.Join(root, "skills")
	store := &workshop.Store{Root: filepath.Join(skillsDir, ".workshop")}
	body := workshop.DraftSkillBody("deploy-check", "Deployment checklist", "1. run tests\n2. deploy staging")
	p, err := store.Create("deploy-check", body, "test", "cli:default", "", false)
	if err != nil {
		t.Fatal(err)
	}
	if p.Meta.Status != workshop.StatusPending {
		t.Fatalf("status=%s", p.Meta.Status)
	}
	if err := p.Apply(skillsDir); err != nil {
		t.Fatal(err)
	}
	skillPath := filepath.Join(skillsDir, "deploy-check", "SKILL.md")
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
	store := &workshop.Store{Root: filepath.Join(skillsDir, ".workshop")}
	body := workshop.DraftSkillBody("deploy-check", "Deployment checklist", "1. run tests")
	p, err := store.Create("deploy-check", body, "test", "cli:default", "", false)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Apply(skillsDir); err == nil {
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
