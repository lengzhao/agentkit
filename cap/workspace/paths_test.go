package workspace_test

import (
	"context"
	"testing"

	"github.com/lengzhao/agentkit/cap/workspace"
)

type stubLayout struct{}

func (stubLayout) Resolve(_ context.Context, rel string) (string, error) { return rel, nil }
func (stubLayout) WorkDirRel() string                                       { return "work" }
func (stubLayout) UploadDirRel() string                                     { return "work/upload" }

func TestParseScoped(t *testing.T) {
	t.Parallel()
	scope, path, ok := workspace.ParseScoped("global:skills")
	if !ok || scope != workspace.ScopeGlobal || path != "skills" {
		t.Fatalf("ParseScoped(global:skills)=%q %q %v", scope, path, ok)
	}
	scope, path, ok = workspace.ParseScoped("local:")
	if !ok || scope != workspace.ScopeLocal || path != "." {
		t.Fatalf("ParseScoped(local:)=%q %q %v", scope, path, ok)
	}
	_, _, ok = workspace.ParseScoped("/abs/path")
	if ok {
		t.Fatal("expected absolute path to be unscoped")
	}
	if !workspace.IsScoped("global:skills") {
		t.Fatal("IsScoped(global:skills) want true")
	}
	if workspace.IsScoped("/abs/path") {
		t.Fatal("IsScoped(/abs/path) want false")
	}
}

func TestFirstScoped(t *testing.T) {
	t.Parallel()

	files := []string{"local:mcp.json", "global:mcp.json", ".cursor/mcp.json"}

	got, err := workspace.FirstScoped(files, workspace.ScopeLocal)
	if err != nil || got != "local:mcp.json" {
		t.Fatalf("local = %q err=%v", got, err)
	}

	got, err = workspace.FirstScoped(files, workspace.ScopeGlobal)
	if err != nil || got != "global:mcp.json" {
		t.Fatalf("global = %q err=%v", got, err)
	}

	_, err = workspace.FirstScoped([]string{"local:mcp.json"}, workspace.ScopeGlobal)
	if err == nil {
		t.Fatal("expected error when no global file configured")
	}

	got, err = workspace.FirstScoped([]string{"mcp.json", "global:mcp.json"}, workspace.ScopeLocal)
	if err != nil || got != "mcp.json" {
		t.Fatalf("bare local = %q err=%v", got, err)
	}

	_, err = workspace.FirstScoped([]string{"https://example.com/mcp.json"}, workspace.ScopeLocal)
	if err == nil {
		t.Fatal("URL must not count as a bare local path")
	}
}

func TestCanonicalWorkPath(t *testing.T) {
	t.Parallel()

	workDir := "work"
	cases := map[string]string{
		"upload/a.jpg":           "work/upload/a.jpg",
		"work/upload/a.jpg":      "work/upload/a.jpg",
		"work/work/upload/a.jpg": "work/upload/a.jpg",
		"/work/upload/a.jpg":     "work/upload/a.jpg",
		"local:skills/foo.md":    "local:skills/foo.md",
	}
	for in, want := range cases {
		if got := workspace.CanonicalWorkPath(workDir, in); got != want {
			t.Fatalf("%q => %q, want %q", in, got, want)
		}
	}
}

func TestNormalizeAgentRel(t *testing.T) {
	t.Parallel()
	workDir := "work"
	if got := workspace.NormalizeAgentRel(workDir, "work/upload/a.jpg"); got != "upload/a.jpg" {
		t.Fatalf("got %q", got)
	}
	if got := workspace.NormalizeAgentRel(workDir, "upload/a.jpg"); got != "upload/a.jpg" {
		t.Fatalf("got %q", got)
	}
}

func TestUploadWorkRel(t *testing.T) {
	t.Parallel()
	ws := stubLayout{}
	if got := workspace.UploadWorkRel(ws); got != "upload" {
		t.Fatalf("got %q", got)
	}
	if got := workspace.AttachFSRel(ws, "a.png"); got != "upload/a.png" {
		t.Fatalf("got %q", got)
	}
}

func TestLocalPath(t *testing.T) {
	t.Parallel()
	if got := workspace.LocalPath("work/upload/a.jpg"); got != "local:work/upload/a.jpg" {
		t.Fatalf("got %q", got)
	}
}

