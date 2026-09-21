package filesystem

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	rtworkspace "github.com/lengzhao/agentkit/runtime/workspace"
)

func TestLocalRootIsTenantLocal(t *testing.T) {
	t.Parallel()

	tenantRoot := t.TempDir()
	workDir := filepath.Join(tenantRoot, "work")
	skillDir := filepath.Join(tenantRoot, "skills", "demo")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	ws := rtworkspace.Static(tenantRoot)
	fs, err := New(Config{Root: "."}, Deps{Workspace: ws})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	if err := fs.Write(ctx, "AGENTS.md", []byte("tenant instructions")); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(tenantRoot, "AGENTS.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "tenant instructions" {
		t.Fatalf("AGENTS.md = %q", got)
	}

	if err := fs.Write(ctx, "skills/demo/reference.md", []byte("# ref")); err != nil {
		t.Fatal(err)
	}
	got, err = os.ReadFile(filepath.Join(skillDir, "reference.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "# ref" {
		t.Fatalf("skills file = %q", got)
	}

	if err := fs.Write(ctx, "work/notes.txt", []byte("temp")); err != nil {
		t.Fatal(err)
	}
	got, err = os.ReadFile(filepath.Join(workDir, "notes.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "temp" {
		t.Fatalf("notes.txt = %q", got)
	}
}

func TestLocalRejectsPathEscapeByDefault(t *testing.T) {
	t.Parallel()

	tenantRoot := t.TempDir()
	workDir := filepath.Join(tenantRoot, "work")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatal(err)
	}
	parentFile := filepath.Join(tenantRoot, "secret.txt")
	if err := os.WriteFile(parentFile, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}

	ws := rtworkspace.Static(tenantRoot)
	fs, err := New(Config{Root: "work"}, Deps{Workspace: ws})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	if _, err := fs.Read(ctx, "../secret.txt"); err == nil {
		t.Fatal("expected path escape error")
	}
	if err := fs.Write(ctx, "../escape.txt", []byte("bad")); err == nil {
		t.Fatal("expected path escape error on write")
	}
}

func TestLocalUnrestrictedPaths(t *testing.T) {
	t.Parallel()

	tenantRoot := t.TempDir()
	workDir := filepath.Join(tenantRoot, "work")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatal(err)
	}
	parentFile := filepath.Join(tenantRoot, "secret.txt")
	if err := os.WriteFile(parentFile, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}

	ws := rtworkspace.Static(tenantRoot)
	fs, err := New(Config{Root: "work", Unrestricted: true}, Deps{Workspace: ws})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	got, err := fs.Read(ctx, "../secret.txt")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "secret" {
		t.Fatalf("secret.txt = %q", got)
	}
	if err := fs.Write(ctx, "../escape.txt", []byte("ok")); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(tenantRoot, "escape.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "ok" {
		t.Fatalf("escape.txt = %q", string(raw))
	}

	absFile := filepath.Join(tenantRoot, "abs.txt")
	if err := os.WriteFile(absFile, []byte("abs"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err = fs.Read(ctx, absFile)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "abs" {
		t.Fatalf("abs file = %q", got)
	}
}

func TestLocalScopedPrefixResolvesViaWorkspace(t *testing.T) {
	t.Parallel()

	global := t.TempDir()
	local := t.TempDir()
	skillDir := filepath.Join(global, "skills", "demo")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "reference.md"), []byte("global-ref"), 0o644); err != nil {
		t.Fatal(err)
	}

	svc, err := rtworkspace.New(rtworkspace.Config{
		Global: global,
		Local:  local,
		Scope:  "local",
	})
	if err != nil {
		t.Fatal(err)
	}
	fs, err := New(Config{Root: "."}, Deps{Workspace: svc})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	// Scoped paths resolve through the workspace (not confined to the fs root).
	got, err := fs.Read(ctx, "global:skills/demo/reference.md")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "global-ref" {
		t.Fatalf("scoped read = %q", got)
	}

	// Writes to a scoped path land in the global root.
	if err := fs.Write(ctx, "global:notes/today.md", []byte("hi")); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(global, "notes", "today.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "hi" {
		t.Fatalf("scoped write = %q", raw)
	}
}

func TestLocalRestrictedAbsolutePathStaysUnderRoot(t *testing.T) {
	t.Parallel()

	tenantRoot := t.TempDir()
	workDir := filepath.Join(tenantRoot, "work")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(tenantRoot, "outside.txt")
	if err := os.WriteFile(outside, []byte("outside"), 0o644); err != nil {
		t.Fatal(err)
	}

	ws := rtworkspace.Static(tenantRoot)
	fs, err := New(Config{Root: "work", Unrestricted: false}, Deps{Workspace: ws})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	_, err = fs.Read(ctx, outside)
	if err == nil {
		t.Fatal("expected restricted absolute path not to read file outside root join semantics")
	}
}
