package config_test

import (
	"strings"
	"testing"

	"github.com/lengzhao/agentkit/config"
	_ "github.com/lengzhao/agentkit/plugins"
)

func TestScaffoldToolsFragmentDefault(t *testing.T) {
	t.Parallel()

	fragment, err := config.ScaffoldToolsFragment(config.ToolProfileDefault, config.ToolsScaffoldOptions{})
	if err != nil {
		t.Fatal(err)
	}

	runtime, ok := fragment["tools.default"].(map[string]any)
	if !ok {
		t.Fatalf("tools.default=%T", fragment["tools.default"])
	}
	if runtime["use"] != "tools/runtime" {
		t.Fatalf("use=%v", runtime["use"])
	}
	deps, ok := runtime["deps"].(map[string]any)
	if !ok {
		t.Fatalf("deps=%T", runtime["deps"])
	}
	for _, id := range []string{
		"tool.shell-bash.default",
		"tool.skill.default",
		"tool.subagent.default",
		"tool.web-fetch-http.default",
		"tool.ask-user.default",
		"tool.send.default",
		"tool.schedule.default",
		"tool.fs-workspace.default",
		"mcp.default",
		"openapi.default",
	} {
		if _, ok := fragment[id]; !ok {
			t.Fatalf("missing instance %q", id)
		}
	}
	tools, ok := deps["tools"].([]string)
	if !ok {
		t.Fatalf("deps.tools=%T", deps["tools"])
	}
	if len(tools) != 7 {
		t.Fatalf("deps.tools=%v want 7 refs", tools)
	}
	for _, bad := range []string{"tool/fs-memory", "tool/finish", "tool/todo"} {
		if _, ok := fragment[defaultToolID(bad)]; ok {
			t.Fatalf("unexpected blacklisted instance for %s", bad)
		}
	}
}

func TestScaffoldToolsFragmentSubagent(t *testing.T) {
	t.Parallel()

	fragment, err := config.ScaffoldToolsFragment(config.ToolProfileSubagent, config.ToolsScaffoldOptions{})
	if err != nil {
		t.Fatal(err)
	}

	runtime, ok := fragment["tools.subagent.default"].(map[string]any)
	if !ok {
		t.Fatalf("tools.subagent.default=%T", fragment["tools.subagent.default"])
	}
	deps, ok := runtime["deps"].(map[string]any)
	if !ok {
		t.Fatalf("deps=%T", runtime["deps"])
	}
	tools, ok := deps["tools"].([]string)
	if !ok {
		t.Fatalf("deps.tools=%T", deps["tools"])
	}
	if len(tools) != 2 {
		t.Fatalf("deps.tools=%v want 2 refs", tools)
	}
	fs, ok := fragment["tool.fs-workspace.readonly.default"].(map[string]any)
	if !ok {
		t.Fatalf("readonly fs=%T", fragment["tool.fs-workspace.readonly.default"])
	}
	cfg, ok := fs["config"].(map[string]any)
	if !ok || cfg["readOnly"] != true {
		t.Fatalf("readonly config=%v", fs["config"])
	}
	for _, id := range []string{
		"tool.subagent.default",
		"tool.shell-bash.default",
		"mcp.default",
	} {
		if _, ok := fragment[id]; ok {
			t.Fatalf("subagent toolset should not include %q", id)
		}
	}
	packs, ok := deps["toolPacks"].([]string)
	if !ok || len(packs) != 1 || packs[0] != "tool.fs-workspace.readonly.default" {
		t.Fatalf("deps.toolPacks=%v", deps["toolPacks"])
	}
}

func TestScaffoldToolsYAML(t *testing.T) {
	t.Parallel()

	raw, err := config.ScaffoldToolsYAML(config.ToolProfileDefault, config.ToolsScaffoldOptions{})
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	if !containsAll(text, "tools.default:", "tool.shell-bash.default:", "use: tool/shell-bash") {
		t.Fatalf("yaml missing expected keys:\n%s", text)
	}
}

func defaultToolID(kind string) string {
	slug := kind
	if len(slug) > 5 && slug[:5] == "tool/" {
		slug = slug[5:]
	}
	return "tool." + slug + ".default"
}

func containsAll(text string, parts ...string) bool {
	for _, part := range parts {
		if !strings.Contains(text, part) {
			return false
		}
	}
	return true
}
