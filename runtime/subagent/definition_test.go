package subagent

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// dirWorkspace maps scoped rels straight to temp dirs, which is all the loader
// needs and keeps these tests independent of workspace/default's root layout.
type dirWorkspace map[string]string

func (w dirWorkspace) Resolve(_ context.Context, rel string) (string, error) {
	if dir, ok := w[rel]; ok {
		return dir, nil
	}
	return "", fmt.Errorf("unknown workspace path %q", rel)
}

func TestParseDefinition(t *testing.T) {
	t.Parallel()

	t.Run("full frontmatter", func(t *testing.T) {
		t.Parallel()
		def, err := parseDefinition("ignored.md", `---
name: researcher
description: read-only research
tools: [read, " grep ", ""]
model: gpt-test
maxSteps: 7
---
You are the research subagent.
`)
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		if def.Name != "researcher" {
			t.Errorf("name = %q", def.Name)
		}
		if def.Description != "read-only research" {
			t.Errorf("description = %q", def.Description)
		}
		if def.Prompt != "You are the research subagent." {
			t.Errorf("prompt = %q", def.Prompt)
		}
		if len(def.Tools) != 2 || def.Tools[0] != "read" || def.Tools[1] != "grep" {
			t.Errorf("tools = %#v, want trimmed [read grep]", def.Tools)
		}
		if def.Model != "gpt-test" {
			t.Errorf("model = %q", def.Model)
		}
		if def.MaxSteps != 7 {
			t.Errorf("maxSteps = %d", def.MaxSteps)
		}
	})

	t.Run("name falls back to file name", func(t *testing.T) {
		t.Parallel()
		def, err := parseDefinition("reviewer.md", "---\ndescription: reviews code\n---\nbody\n")
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		if def.Name != "reviewer" {
			t.Errorf("name = %q, want reviewer", def.Name)
		}
	})

	t.Run("no frontmatter", func(t *testing.T) {
		t.Parallel()
		// Everything is body, so the failure is the missing description rather
		// than a YAML error — that is what makes the message actionable.
		_, err := parseDefinition("plain.md", "just a prompt, no frontmatter\n")
		if !errors.Is(err, errNoDescription) {
			t.Fatalf("err = %v, want errNoDescription", err)
		}
	})

	t.Run("unterminated frontmatter", func(t *testing.T) {
		t.Parallel()
		_, err := parseDefinition("open.md", "---\ndescription: never closed\nbody text\n")
		if !errors.Is(err, errNoDescription) {
			t.Fatalf("err = %v, want errNoDescription", err)
		}
	})

	t.Run("missing description", func(t *testing.T) {
		t.Parallel()
		_, err := parseDefinition("x.md", "---\nname: x\n---\nbody\n")
		if !errors.Is(err, errNoDescription) {
			t.Fatalf("err = %v, want errNoDescription", err)
		}
	})

	t.Run("empty body", func(t *testing.T) {
		t.Parallel()
		_, err := parseDefinition("x.md", "---\ndescription: has one\n---\n\n   \n")
		if !errors.Is(err, errNoPrompt) {
			t.Fatalf("err = %v, want errNoPrompt", err)
		}
	})

	t.Run("broken yaml", func(t *testing.T) {
		t.Parallel()
		if _, err := parseDefinition("x.md", "---\ntools: [unclosed\n---\nbody\n"); err == nil {
			t.Fatal("expected a yaml error")
		}
	})
}

func TestLoadDefinitionsFirstDirWins(t *testing.T) {
	t.Parallel()

	local, global := t.TempDir(), t.TempDir()
	writeDef(t, local, "researcher.md", "---\ndescription: local version\n---\nlocal body\n")
	writeDef(t, global, "researcher.md", "---\ndescription: global version\n---\nglobal body\n")
	writeDef(t, global, "reviewer.md", "---\ndescription: only global\n---\nreview body\n")
	// Malformed and non-markdown files must not break the rest of the catalog.
	writeDef(t, local, "broken.md", "---\nname: broken\n---\nno description\n")
	writeDef(t, local, "notes.txt", "description: not markdown\n")

	ws := dirWorkspace{"local:agents": local, "global:agents": global}
	defs, err := loadDefinitions(context.Background(), ws, []string{"local:agents", "global:agents", "missing:dir"})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(defs) != 2 {
		t.Fatalf("got %d definitions, want 2: %#v", len(defs), defs)
	}
	// Sorted by name, so researcher comes first.
	if defs[0].Name != "researcher" || defs[1].Name != "reviewer" {
		t.Fatalf("names = %q, %q, want researcher, reviewer", defs[0].Name, defs[1].Name)
	}
	if defs[0].Description != "local version" {
		t.Errorf("description = %q, want the local directory to win", defs[0].Description)
	}
	if want := filepath.Join(local, "researcher.md"); defs[0].Path != want {
		t.Errorf("path = %q, want %q", defs[0].Path, want)
	}
}

func TestLoadDefinitionsNoDirs(t *testing.T) {
	t.Parallel()

	defs, err := loadDefinitions(context.Background(), dirWorkspace{}, []string{"local:agents"})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(defs) != 0 {
		t.Fatalf("got %d definitions, want none", len(defs))
	}
}

func writeDef(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}
