package smoke_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/plugins/tool/fs"
	rtworkspace "github.com/lengzhao/agentkit/runtime/workspace"
	"github.com/lengzhao/agentkit/testing/agenttest"
)

// E2E-011: grep/find respect workspace .gitignore (ignored paths must not surface).
func TestSmokeGitignoreHidesIgnoredPathsFromFindAndGrep(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte("secrets/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "secrets"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "secrets", "leak.txt"), []byte("top-secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "visible.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	ws := rtworkspace.Static(root)
	pack, err := fs.NewFSWorkspace(fs.FSWorkspaceConfig{
		Tools: []string{"grep", "find"},
	}, fs.FSWorkspaceDeps{
		FS:        smokeFS(t, root),
		Workspace: ws,
	})
	if err != nil {
		t.Fatal(err)
	}
	var grepTool, findTool agentkit.Tool
	for _, tool := range pack {
		switch tool.Name() {
		case "grep":
			grepTool = tool
		case "find":
			findTool = tool
		}
	}
	if grepTool == nil || findTool == nil {
		t.Fatal("expected grep and find tools")
	}

	ctx := agenttest.TurnContext(agentkit.SessionID("smoke:gitignore"), agentkit.AgentID("smoke"))

	findOut := agenttest.CallTool(t, ctx, findTool, `{"pattern":"**/*.txt"}`)
	if strings.Contains(findOut, "secrets") || strings.Contains(findOut, "leak.txt") {
		t.Fatalf("find must not list gitignored paths: %q", findOut)
	}
	if !strings.Contains(findOut, "visible.txt") {
		t.Fatalf("find should list visible.txt: %q", findOut)
	}

	grepOut := agenttest.CallTool(t, ctx, grepTool, `{"pattern":"top-secret","path":"."}`)
	if strings.Contains(grepOut, "leak.txt") || strings.Contains(grepOut, "top-secret") {
		t.Fatalf("grep must not match gitignored file contents: %q", grepOut)
	}
}
