package media_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	rtmedia "github.com/lengzhao/agentkit/runtime/media"
	rtworkspace "github.com/lengzhao/agentkit/runtime/workspace"
)

func TestAgentLLMPathWorkResolvesAbsolute(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	ws := rtworkspace.Static(root)
	ctx := context.Background()
	wantUpload := filepath.Join(root, "work", "upload", "a.png")
	cases := map[string]string{
		"local:work/upload/a.png": wantUpload,
		"work/upload/a.png":       wantUpload,
		"upload/a.png":            wantUpload,
	}
	for in, want := range cases {
		if got := rtmedia.AgentLLMPath(ctx, ws, in); got != want {
			t.Fatalf("%q => %q, want %q", in, got, want)
		}
	}
}

func TestAgentLLMPathGlobalResolvesAbsolute(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	globalRoot := filepath.Join(root, "global")
	if err := os.MkdirAll(filepath.Join(globalRoot, "skills"), 0o755); err != nil {
		t.Fatal(err)
	}
	ws := scopedRoots{local: filepath.Join(root, "tenant"), global: globalRoot}
	ctx := context.Background()
	got := rtmedia.AgentLLMPath(ctx, ws, "global:skills/demo.md")
	want := filepath.Join(globalRoot, "skills", "demo.md")
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestRewritePathsInTextStripsLocalPrefix(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	ws := rtworkspace.Static(root)
	ctx := context.Background()
	want := filepath.Join(root, "work", "upload", "a.png")
	out := rtmedia.RewritePathsInText(ctx, ws, "file local:work/upload/a.png here")
	if strings.Contains(out, "local:") {
		t.Fatalf("got %q", out)
	}
	if !strings.Contains(out, want) {
		t.Fatalf("got %q, want substring %q", out, want)
	}
}

type scopedRoots struct {
	local  string
	global string
}

func (s scopedRoots) Resolve(_ context.Context, rel string) (string, error) {
	if strings.HasPrefix(rel, "global:") {
		return filepath.Join(s.global, filepath.FromSlash(rel[len("global:"):])), nil
	}
	if strings.HasPrefix(rel, "local:") {
		return filepath.Join(s.local, filepath.FromSlash(rel[len("local:"):])), nil
	}
	return filepath.Join(s.local, rel), nil
}

func (s scopedRoots) WorkDirRel() string   { return "work" }
func (s scopedRoots) UploadDirRel() string { return "work/upload" }
