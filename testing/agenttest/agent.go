package agenttest

import (
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/prompt"
	"github.com/lengzhao/agentkit/runtime/tools"
)

// EmptyToolsRuntime returns a tool runtime with no tools and allow-all approval.
func EmptyToolsRuntime(t *testing.T) agentkit.ToolRuntime {
	t.Helper()
	rt, err := tools.NewRuntime(tools.RuntimeConfig{}, tools.RuntimeDeps{Approval: AllowAll{}})
	if err != nil {
		t.Fatal(err)
	}
	return rt
}

// DefaultAssembler builds the standard prompt assembler used in smoke tests.
func DefaultAssembler(t *testing.T) agentkit.PromptAssembler {
	t.Helper()
	assembler, err := prompt.NewAssembler(prompt.AssemblerConfig{}, prompt.AssemblerDeps{})
	if err != nil {
		t.Fatal(err)
	}
	return assembler
}
