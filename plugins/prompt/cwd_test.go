package prompt

import (
	"context"
	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/workspace"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAgentsMDUsesTurnCwd(t *testing.T) {
	root := t.TempDir()
	project := t.TempDir()
	if err := os.WriteFile(filepath.Join(project, "AGENTS.md"), []byte("PROJECT_INSTRUCTIONS"), 0600); err != nil {
		t.Fatal(err)
	}
	p, err := NewAgentsMD(AgentsMDConfig{}, newTestFileDeps(t, root))
	if err != nil {
		t.Fatal(err)
	}
	section, err := p.Sections()[0].Build(workspace.WithWorkingDirectory(context.Background(), project), agentkit.PromptRequest{})
	if err != nil || !strings.Contains(section.Content, "PROJECT_INSTRUCTIONS") {
		t.Fatalf("section %q %v", section.Content, err)
	}
}
