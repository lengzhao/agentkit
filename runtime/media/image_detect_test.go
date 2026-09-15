package media_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	rtmedia "github.com/lengzhao/agentkit/runtime/media"
	rtworkspace "github.com/lengzhao/agentkit/runtime/workspace"
)

func TestEnsureInboundFileNameAddsJPEGExtension(t *testing.T) {
	t.Parallel()
	got := rtmedia.EnsureInboundFileName("file_123_0", "image/jpeg", []byte{0xFF, 0xD8, 0xFF})
	if got != "file_123_0.jpg" {
		t.Fatalf("name = %q", got)
	}
}

func TestWorkspaceFileMayBeImageExtensionlessJPEG(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	workDir := filepath.Join(root, "work", "upload")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workDir, "file_1_0"), []byte{0xFF, 0xD8, 0xFF, 0x00}, 0o644); err != nil {
		t.Fatal(err)
	}
	ws := rtworkspace.Static(root)
	ok, err := rtmedia.WorkspaceFileMayBeImage(context.Background(), ws, "work/upload/file_1_0")
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected extensionless jpeg to be detected")
	}
}
