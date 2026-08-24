package fsreadonly_test

import (
	"context"
	"strings"
	"testing"

	"github.com/lengzhao/agentkit/cap/filesystem"
	"github.com/lengzhao/agentkit/cap/workspace"
	"github.com/lengzhao/agentkit/plugins/fsreadonly"
	"github.com/lengzhao/agentkit/plugins/fslocal"
)

func TestReadonlyAllowsReadDeniesWrite(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	inner, err := fslocal.New(fslocal.Config{Root: "."}, fslocal.Deps{Workspace: workspace.Static(dir)})
	if err != nil {
		t.Fatalf("local fs: %v", err)
	}
	if err := inner.WriteText(context.Background(), "a.txt", "hello"); err != nil {
		t.Fatalf("seed write: %v", err)
	}

	ro, err := fsreadonly.New(fsreadonly.Config{}, fsreadonly.Deps{FS: inner})
	if err != nil {
		t.Fatalf("readonly fs: %v", err)
	}

	text, err := ro.ReadText(context.Background(), "a.txt", 0)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if text != "hello" {
		t.Fatalf("read text = %q", text)
	}
	if err := ro.WriteText(context.Background(), "b.txt", "nope"); err == nil {
		t.Fatal("expected write error")
	} else if !strings.Contains(err.Error(), "read-only") {
		t.Fatalf("unexpected write error: %v", err)
	}
	_, err = ro.Edit(context.Background(), filesystem.EditRequest{
		Path:      "a.txt",
		OldString: "hello",
		NewString: "world",
	})
	if err == nil {
		t.Fatal("expected edit error")
	}
}
