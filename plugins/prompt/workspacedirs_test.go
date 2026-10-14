package prompt

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lengzhao/agentkit/cap/workspace"
	rtworkspace "github.com/lengzhao/agentkit/runtime/workspace"
)

func newDualRootWorkspace(t *testing.T) (workspace.Service, string, string) {
	t.Helper()
	global := t.TempDir()
	local := t.TempDir()
	ws, err := rtworkspace.New(rtworkspace.Config{Global: global, Local: local, Scope: "local"})
	if err != nil {
		t.Fatal(err)
	}
	return ws, global, local
}

func TestWorkspaceDirsResolvesScopesPerEntry(t *testing.T) {
	t.Parallel()

	ws, global, local := newDualRootWorkspace(t)
	provider, err := NewWorkspaceDirs(WorkspaceDirsConfig{
		Intro: "可写目录：",
		Entries: []WorkspaceDirEntry{{
			Label: "workflows",
			Dirs:  []string{"local:workflows", "global:workflows"},
			Note:  "同名文件先命中者优先。",
		}},
	}, WorkspaceDirsDeps{Workspace: ws})
	if err != nil {
		t.Fatal(err)
	}
	section := buildSection(t, provider)
	if section.Name != "workspace-dirs" {
		t.Fatalf("section name = %q, want %q", section.Name, "workspace-dirs")
	}
	wantLocal := filepath.Join(local, "workflows") + " (local)"
	wantGlobal := filepath.Join(global, "workflows") + " (global)"
	if !strings.Contains(section.Content, wantLocal) {
		t.Fatalf("content missing %q:\n%s", wantLocal, section.Content)
	}
	if !strings.Contains(section.Content, wantGlobal) {
		t.Fatalf("content missing %q:\n%s", wantGlobal, section.Content)
	}
	if strings.Index(section.Content, wantLocal) > strings.Index(section.Content, wantGlobal) {
		t.Fatalf("local dir should render before global:\n%s", section.Content)
	}
	if !strings.Contains(section.Content, "可写目录：") || !strings.Contains(section.Content, "同名文件先命中者优先。") {
		t.Fatalf("content missing intro or note:\n%s", section.Content)
	}
}

func TestWorkspaceDirsUnscopedDirHasNoScopeSuffix(t *testing.T) {
	t.Parallel()

	ws, _, local := newDualRootWorkspace(t)
	provider, err := NewWorkspaceDirs(WorkspaceDirsConfig{
		Entries: []WorkspaceDirEntry{{
			Label: "uploads",
			Dirs:  []string{"work/upload"},
		}},
	}, WorkspaceDirsDeps{Workspace: ws})
	if err != nil {
		t.Fatal(err)
	}
	section := buildSection(t, provider)
	want := filepath.Join(local, "work", "upload")
	if !strings.Contains(section.Content, want) {
		t.Fatalf("content missing %q:\n%s", want, section.Content)
	}
	if strings.Contains(section.Content, "(local)") || strings.Contains(section.Content, "(global)") {
		t.Fatalf("unscoped dir must not carry a scope suffix:\n%s", section.Content)
	}
}

type failingWorkspace struct{ err error }

func (w failingWorkspace) Resolve(context.Context, string) (string, error) { return "", w.err }

func TestWorkspaceDirsSkipsUnresolvableDirs(t *testing.T) {
	t.Parallel()

	provider, err := NewWorkspaceDirs(WorkspaceDirsConfig{
		Intro:   "intro",
		Entries: []WorkspaceDirEntry{{Label: "workflows", Dirs: []string{"local:workflows"}}},
	}, WorkspaceDirsDeps{Workspace: failingWorkspace{err: errors.New("boom")}})
	if err != nil {
		t.Fatal(err)
	}
	// 全部解析失败时 section 为空（含 intro），assembler 会跳过它。
	if section := buildSection(t, provider); section.Content != "" {
		t.Fatalf("content = %q, want empty", section.Content)
	}
}

func TestNewWorkspaceDirsValidatesConfigAndDeps(t *testing.T) {
	t.Parallel()

	ws, _, _ := newDualRootWorkspace(t)
	entry := WorkspaceDirEntry{Label: "workflows", Dirs: []string{"local:workflows"}}
	if _, err := NewWorkspaceDirs(WorkspaceDirsConfig{Entries: []WorkspaceDirEntry{entry}}, WorkspaceDirsDeps{}); err == nil {
		t.Fatal("expected error for missing workspace")
	}
	if _, err := NewWorkspaceDirs(WorkspaceDirsConfig{}, WorkspaceDirsDeps{Workspace: ws}); err == nil {
		t.Fatal("expected error for empty entries")
	}
	if _, err := NewWorkspaceDirs(WorkspaceDirsConfig{
		Entries: []WorkspaceDirEntry{{Dirs: []string{"local:workflows"}}},
	}, WorkspaceDirsDeps{Workspace: ws}); err == nil {
		t.Fatal("expected error for empty label")
	}
	if _, err := NewWorkspaceDirs(WorkspaceDirsConfig{
		Entries: []WorkspaceDirEntry{{Label: "workflows"}},
	}, WorkspaceDirsDeps{Workspace: ws}); err == nil {
		t.Fatal("expected error for empty dirs")
	}
}
