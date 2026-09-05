package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSamePluginTree(t *testing.T) {
	t.Parallel()

	cases := []struct {
		pkg, imp string
		want     bool
	}{
		{"learning", "learning", true},
		{"learning", "learning/dreaming", true},
		{"learning/dreaming", "learning", true},
		{"tool/schedule", "schedule", false},
		{"prompt", "learning", false},
	}
	for _, tc := range cases {
		got := samePluginTree(tc.pkg, tc.imp)
		if got != tc.want {
			t.Fatalf("samePluginTree(%q, %q) = %v, want %v", tc.pkg, tc.imp, got, tc.want)
		}
	}
}

func TestCheckPluginImportsAllowsSameTree(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeFile(t, filepath.Join(root, "plugins", "learning", "service.go"), `package learning

import "github.com/lengzhao/agentkit/plugins/learning/dreaming"
`)
	writeFile(t, filepath.Join(root, "plugins", "learning", "dreaming", "sweep.go"), `package dreaming
`)
	violations, err := checkPluginImports(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(violations) != 0 {
		t.Fatalf("expected no violations, got %#v", violations)
	}
}

func TestCheckPluginImportsRejectsCrossPlugin(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeFile(t, filepath.Join(root, "plugins", "prompt", "memorymd.go"), `package prompt

import "github.com/lengzhao/agentkit/plugins/learning"
`)
	writeFile(t, filepath.Join(root, "plugins", "learning", "service.go"), `package learning
`)
	violations, err := checkPluginImports(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(violations) != 1 {
		t.Fatalf("expected 1 violation, got %#v", violations)
	}
	if violations[0].File != "plugins/prompt/memorymd.go" {
		t.Fatalf("unexpected file: %s", violations[0].File)
	}
}

func TestCheckPluginImportsSkipsAllGo(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeFile(t, filepath.Join(root, "plugins", "all.go"), `package plugins

import _ "github.com/lengzhao/agentkit/plugins/learning"
`)
	violations, err := checkPluginImports(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(violations) != 0 {
		t.Fatalf("expected all.go to be skipped, got %#v", violations)
	}
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
