package memory

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	rtworkspace "github.com/lengzhao/agentkit/runtime/workspace"
)

func TestMemoryCommandAddAndShow(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	ws, err := rtworkspace.New(rtworkspace.Config{Global: root, Local: root, Scope: "local"})
	if err != nil {
		t.Fatal(err)
	}
	svc, err := New(Config{}, Deps{FS: testFS(t, ws)})
	if err != nil {
		t.Fatal(err)
	}
	cmd := svc.Commands()[0]
	out, err := cmd.CommandExec(context.Background(), "add likes Go tests")
	if err != nil {
		t.Fatal(err)
	}
	if out == "" {
		t.Fatal("expected confirmation")
	}
	show, err := cmd.CommandExec(context.Background(), "show")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(show, "likes Go tests") {
		t.Fatalf("show = %q", show)
	}
}

func TestMemoryAddWritesLedgerNotSourceInMarkdown(t *testing.T) {
	dir := t.TempDir()
	ws, err := rtworkspace.New(rtworkspace.Config{Local: dir, Scope: "local"})
	if err != nil {
		t.Fatal(err)
	}
	svc, err := New(Config{}, Deps{FS: testFS(t, ws)})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := svc.addMemory(ctx, "likes tea", "background-review"); err != nil {
		t.Fatal(err)
	}
	memPath := filepath.Join(dir, "memory.md")
	data, err := os.ReadFile(memPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "<!--") || strings.Contains(string(data), "source=") {
		t.Fatalf("memory.md should not contain source metadata: %q", data)
	}
	ledgerPath := filepath.Join(dir, "memory", "ledger.jsonl")
	ledger, err := os.ReadFile(ledgerPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(ledger), "background-review") {
		t.Fatalf("ledger = %q", ledger)
	}
}

func TestPolicyMemoryApproveAndAuto(t *testing.T) {
	dir := t.TempDir()
	ws := rtworkspace.Static(dir)
	svc, err := New(Config{Review: ReviewConfig{}}, Deps{FS: testFS(t, ws)})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := svc.handleMemoryPolicy(ctx, []string{"auto"}); err != nil {
		t.Fatal(err)
	}
	if svc.memoryWriteRequiresApproval(ctx, "background-review") {
		t.Fatal("expected auto memory write")
	}
	if _, err := svc.handleMemoryPolicy(ctx, []string{"approve"}); err != nil {
		t.Fatal(err)
	}
	if !svc.memoryWriteRequiresApproval(ctx, "background-review") {
		t.Fatal("expected staged memory")
	}
}
