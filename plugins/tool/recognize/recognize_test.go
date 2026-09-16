package recognize_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lengzhao/agentkit"
	recognizeplugin "github.com/lengzhao/agentkit/plugins/tool/recognize"
	"github.com/lengzhao/agentkit/runtime/llm"
	"github.com/lengzhao/agentkit/runtime/workspace"
)

func TestRecognizeImageScripted(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	png := []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a}
	workDir := filepath.Join(root, "work")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workDir, "shot.png"), png, 0o644); err != nil {
		t.Fatal(err)
	}
	ws := workspace.Static(root)

	provider, err := llm.NewScripted(llm.ScriptedConfig{
		Steps: []llm.ScriptedStep{{Text: "a red button"}},
	})
	if err != nil {
		t.Fatal(err)
	}

	pack, err := recognizeplugin.NewRecognize(recognizeplugin.RecognizeConfig{}, recognizeplugin.RecognizeDeps{
		LLM:       provider,
		Workspace: ws,
	})
	if err != nil {
		t.Fatal(err)
	}
	var tool agentkit.Tool
	for _, item := range pack {
		if item.Name() == "recognize_image" {
			tool = item
			break
		}
	}
	if tool == nil {
		t.Fatal("recognize_image not found")
	}
	out, err := tool.Call(context.Background(), json.RawMessage(`{"path":"shot.png","task":"what is visible?"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "red button") {
		t.Fatalf("out = %q", out)
	}
}

