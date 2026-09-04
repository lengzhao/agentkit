package skill_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lengzhao/agentkit"
	capskill "github.com/lengzhao/agentkit/cap/skill"
	skilltool "github.com/lengzhao/agentkit/plugins/tool/skill"
	"github.com/lengzhao/agentkit/testing/agenttest"
)

type dirRegistry struct {
	root string
}

func (r dirRegistry) List(context.Context) ([]capskill.Descriptor, error) {
	return []capskill.Descriptor{{
		Name:        "demo",
		Description: "Demo skill",
		Path:        filepath.Join(r.root, "demo"),
	}}, nil
}

func (r dirRegistry) Load(_ context.Context, name string) (capskill.Content, error) {
	if name != "demo" {
		return capskill.Content{}, os.ErrNotExist
	}
	dir := filepath.Join(r.root, "demo")
	body, err := os.ReadFile(filepath.Join(dir, "SKILL.md"))
	if err != nil {
		return capskill.Content{}, err
	}
	return capskill.Content{
		Name:        "demo",
		Description: "Demo skill",
		Body:        string(body),
		Path:        dir,
	}, nil
}

func TestSkillToolLoadsSupportingFile(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	skillDir := filepath.Join(root, "demo")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("# Demo\nUse reference.md"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "reference.md"), []byte("# Reference\nMore detail"), 0o644); err != nil {
		t.Fatal(err)
	}

	store, _ := agenttest.TempFileStore(t)
	tool, err := skilltool.NewSkill(skilltool.SkillConfig{}, skilltool.SkillDeps{
		Skills:       dirRegistry{root: root},
		SessionStore: store,
	})
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.WithValue(context.Background(), agentkit.KeySessionID, agentkit.SessionID("skill-file"))
	ctx = context.WithValue(ctx, agentkit.KeyAgentID, agentkit.AgentID("assistant"))
	out, err := tool.Call(ctx, []byte(`{"name":"demo","file":"reference.md"}`))
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if !strings.Contains(out, `<skill_resource name="demo" file="reference.md">`) {
		t.Fatalf("out = %q", out)
	}
	if !strings.Contains(out, "More detail") {
		t.Fatalf("out = %q", out)
	}
}

func TestSkillToolRunsScript(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	skillDir := filepath.Join(root, "demo", "scripts")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "demo", "SKILL.md"), []byte("# Demo\nRun scripts/run.sh"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "run.sh"), []byte("#!/usr/bin/env bash\nprintf 'skill-script-ok %s' \"$1\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	tool, err := skilltool.NewSkill(skilltool.SkillConfig{}, skilltool.SkillDeps{
		Skills: dirRegistry{root: root},
	})
	if err != nil {
		t.Fatal(err)
	}

	out, err := tool.Call(context.Background(), []byte(`{"name":"demo","script":"scripts/run.sh","args":["arg1"]}`))
	if err != nil {
		t.Fatalf("call: %v, out=%q", err, out)
	}
	if !strings.Contains(out, `<skill_script name="demo" script="scripts/run.sh">`) {
		t.Fatalf("out = %q", out)
	}
	if !strings.Contains(out, "skill-script-ok arg1") {
		t.Fatalf("out = %q", out)
	}
	if !strings.Contains(out, "exitCode: 0") {
		t.Fatalf("out = %q", out)
	}
}
