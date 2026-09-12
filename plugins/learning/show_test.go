package learning

import (
	"context"
	"strings"
	"testing"

	rtworkspace "github.com/lengzhao/agentkit/runtime/workspace"
)

func TestLearnShowIncludesMemory(t *testing.T) {
	dir := t.TempDir()
	ws, err := rtworkspace.New(rtworkspace.Config{Local: dir, Scope: "local"})
	if err != nil {
		t.Fatal(err)
	}
	svc, err := New(Config{}, Deps{Workspace: ws, SessionStore: stubSessionStore{}})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := svc.addMemory(ctx, "call me 小飞", "test"); err != nil {
		t.Fatal(err)
	}
	out, err := svc.Commands()[0].CommandExec(ctx, "show")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "personal memory") || !strings.Contains(out, "小飞") {
		t.Fatalf("show = %q", out)
	}
}
