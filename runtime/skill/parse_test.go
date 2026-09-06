package skill_test

import (
	"strings"
	"testing"

	rtskill "github.com/lengzhao/agentkit/runtime/skill"
)

func TestParseFileValid(t *testing.T) {
	t.Parallel()

	raw := strings.Join([]string{
		"---",
		"name: rich-skill",
		"description: rich description",
		"license: MIT",
		"compatibility: Requires git",
		"allowed-tools: read bash",
		"whenToUse: For richer local parsing",
		"disable-model-invocation: off",
		"user-invocable: YES",
		"metadata:",
		"  owner: tests",
		"---",
		"",
		"Rich body.",
	}, "\n")
	parsed, err := rtskill.ParseFile(raw, rtskill.ParseOptions{DirName: "rich-skill"})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if parsed.Name != "rich-skill" {
		t.Fatalf("name = %q", parsed.Name)
	}
	if parsed.Description != "rich description" {
		t.Fatalf("description = %q", parsed.Description)
	}
	if parsed.License != "MIT" {
		t.Fatalf("license = %q", parsed.License)
	}
	if parsed.Compatibility != "Requires git" {
		t.Fatalf("compatibility = %q", parsed.Compatibility)
	}
	if parsed.AllowedTools != "read bash" {
		t.Fatalf("allowed-tools = %q", parsed.AllowedTools)
	}
	if parsed.WhenToUse != "For richer local parsing" {
		t.Fatalf("whenToUse = %q", parsed.WhenToUse)
	}
	if !parsed.Invocation.ModelInvocable || !parsed.Invocation.UserInvocable {
		t.Fatalf("invocation = %#v", parsed.Invocation)
	}
	if parsed.Metadata["owner"] != "tests" {
		t.Fatalf("metadata = %#v", parsed.Metadata)
	}
	if parsed.Content != "Rich body." {
		t.Fatalf("content = %q", parsed.Content)
	}
}

func TestParseFileRequiresFrontmatter(t *testing.T) {
	t.Parallel()

	_, err := rtskill.ParseFile("# Title\nNo frontmatter.", rtskill.ParseOptions{})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestParseFileInvalidName(t *testing.T) {
	t.Parallel()

	raw := "---\nname: Bad_Name\ndescription: bad\n---\n\nbad\n"
	_, err := rtskill.ParseFile(raw, rtskill.ParseOptions{})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestParseFileNameMustMatchDirectory(t *testing.T) {
	t.Parallel()

	raw := "---\nname: bundle-skill\ndescription: ok\n---\n\nbody\n"
	_, err := rtskill.ParseFile(raw, rtskill.ParseOptions{DirName: "other-name"})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestParseFileNameTooLong(t *testing.T) {
	t.Parallel()

	name := strings.Repeat("a", 65)
	raw := "---\nname: " + name + "\ndescription: ok\n---\n\nbody\n"
	_, err := rtskill.ParseFile(raw, rtskill.ParseOptions{})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestParseFileDescriptionTooLong(t *testing.T) {
	t.Parallel()

	desc := strings.Repeat("a", 1025)
	raw := "---\nname: demo\ndescription: " + desc + "\n---\n\nbody\n"
	_, err := rtskill.ParseFile(raw, rtskill.ParseOptions{})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestParseFileMetadataMustBeStrings(t *testing.T) {
	t.Parallel()

	raw := "---\nname: demo\ndescription: ok\nmetadata:\n  version: 1\n---\n\nbody\n"
	_, err := rtskill.ParseFile(raw, rtskill.ParseOptions{})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestParseFileInvocationPolicy(t *testing.T) {
	t.Parallel()

	userOnly := "---\nname: user-only\ndescription: user-only\ndisable-model-invocation: true\n---\n\nUser-only.\n"
	parsed, err := rtskill.ParseFile(userOnly, rtskill.ParseOptions{})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if parsed.Invocation.ModelInvocable || !parsed.Invocation.UserInvocable {
		t.Fatalf("invocation = %#v", parsed.Invocation)
	}

	modelOnly := "---\nname: model-only\ndescription: model-only\nuser-invocable: false\n---\n\nModel-only.\n"
	parsed, err = rtskill.ParseFile(modelOnly, rtskill.ParseOptions{})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !parsed.Invocation.ModelInvocable || parsed.Invocation.UserInvocable {
		t.Fatalf("invocation = %#v", parsed.Invocation)
	}
}

func TestParseFileRejectsLegacyInvocationKeys(t *testing.T) {
	t.Parallel()

	raw := "---\nname: legacy\ndescription: legacy\ndisableModelInvocation: true\n---\n\nBad.\n"
	_, err := rtskill.ParseFile(raw, rtskill.ParseOptions{})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestIsName(t *testing.T) {
	t.Parallel()

	if !rtskill.IsName("agentkit-config") {
		t.Fatal("expected valid name")
	}
	if rtskill.IsName("Bad_Name") {
		t.Fatal("expected invalid name")
	}
	if rtskill.IsName("pdf--processing") {
		t.Fatal("expected invalid name for consecutive hyphens")
	}
}
