package learning

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/lengzhao/agentkit/plugins/learning/workshop"
	rtworkspace "github.com/lengzhao/agentkit/runtime/workspace"
)

func TestDefaultMemoryPolicyIsAuto(t *testing.T) {
	if defaultMemoryWriteMode(ReviewServiceConfig{}) != "auto" {
		t.Fatal("expected default auto")
	}
}

func TestPolicyMemoryApproveAndAuto(t *testing.T) {
	dir := t.TempDir()
	svc := &Service{
		memoryRoot: ".",
		review:     ReviewServiceConfig{},
		workspace:  rtworkspace.Static(dir),
	}
	ctx := context.Background()

	if _, err := svc.setMemoryPolicy(ctx, []string{"auto"}); err != nil {
		t.Fatal(err)
	}
	if svc.memoryWriteRequiresApproval(ctx, "background-review") {
		t.Fatal("expected auto memory write")
	}
	if _, err := svc.setMemoryPolicy(ctx, []string{"approve"}); err != nil {
		t.Fatal(err)
	}
	if !svc.memoryWriteRequiresApproval(ctx, "background-review") {
		t.Fatal("expected staged memory")
	}
	policyPath := filepath.Join(dir, "memory/learning/policy.json")
	if _, err := os.Stat(policyPath); err != nil {
		t.Fatalf("policy file: %v", err)
	}
}

func TestPolicySkillsMode(t *testing.T) {
	dir := t.TempDir()
	svc := &Service{
		memoryRoot: ".",
		workshop:   workshop.Config{Mode: "propose"},
		workspace:  rtworkspace.Static(dir),
	}
	ctx := context.Background()
	if _, err := svc.setSkillsPolicy(ctx, []string{"off"}); err != nil {
		t.Fatal(err)
	}
	if svc.skillsWorkshopEnabled(ctx) {
		t.Fatal("expected skills off")
	}
}
