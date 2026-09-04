package skill

import (
	"strings"
	"testing"
)

func TestRenderLoadedIncludesResourceBase(t *testing.T) {
	t.Parallel()

	text := RenderLoaded(Content{
		Name: "demo",
		Body: "Do the thing.",
		Path: "/tmp/skills/demo",
	})
	if !strings.Contains(text, `<skill_content name="demo">`) {
		t.Fatalf("text = %q", text)
	}
	if !strings.Contains(text, "Base directory for this skill: /tmp/skills/demo") {
		t.Fatalf("text = %q", text)
	}
	if !strings.Contains(text, "Do the thing.") {
		t.Fatalf("text = %q", text)
	}
}

func TestRenderResourceFile(t *testing.T) {
	t.Parallel()

	text := RenderResourceFile("demo", "/tmp/skills/demo", "reference.md", "# Ref")
	if !strings.Contains(text, `file="reference.md"`) {
		t.Fatalf("text = %q", text)
	}
	if !strings.Contains(text, "# Ref") {
		t.Fatalf("text = %q", text)
	}
}

func TestSanitizeRelativePath(t *testing.T) {
	t.Parallel()

	if _, err := SanitizeRelativePath("../secret.md"); err == nil {
		t.Fatal("expected escape rejection")
	}
	if got, err := SanitizeRelativePath("reference.md"); err != nil || got != "reference.md" {
		t.Fatalf("got %q err=%v", got, err)
	}
}
