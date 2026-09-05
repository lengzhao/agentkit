package skill_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	capsskill "github.com/lengzhao/agentkit/cap/skill"
	rtskill "github.com/lengzhao/agentkit/runtime/skill"
)

type staticRegistry struct {
	dir string
}

func (r staticRegistry) List(context.Context) ([]capsskill.Descriptor, error) {
	return []capsskill.Descriptor{{Name: "demo", Description: "demo", Path: r.dir}}, nil
}

func (r staticRegistry) Load(context.Context, string) (capsskill.Content, error) {
	return capsskill.Content{Name: "demo", Description: "demo", Path: r.dir}, nil
}

func TestRunScript(t *testing.T) {
	t.Parallel()

	dir := filepath.Join(t.TempDir(), "demo", "scripts")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(dir, "echo.sh")
	if err := os.WriteFile(script, []byte("#!/usr/bin/env bash\necho hi-from-script\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	result, err := rtskill.RunScript(context.Background(), staticRegistry{dir: filepath.Dir(dir)}, "demo", "scripts/echo.sh", nil, 0)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if result.ExitCode != 0 {
		t.Fatalf("exit = %d", result.ExitCode)
	}
	if result.Stdout != "hi-from-script\n" {
		t.Fatalf("stdout = %q", result.Stdout)
	}
}

func TestRunScriptRejectsEscape(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	_, err := rtskill.RunScript(context.Background(), staticRegistry{dir: dir}, "demo", "../secret.sh", nil, 0)
	if err == nil {
		t.Fatal("expected path escape rejection")
	}
}
