package agent_test

import (
	"context"
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/workspace"
	"github.com/lengzhao/agentkit/runtime/llm"
	"github.com/lengzhao/agentkit/runtime/rctx"
)

type cwdProbeLLM struct {
	t    *testing.T
	want string
	next agentkit.LLMProvider
}

func (p cwdProbeLLM) Name() string { return "cwd-probe" }
func (p cwdProbeLLM) Stream(ctx context.Context, req agentkit.LLMRequest) (agentkit.LLMStream, error) {
	if workspace.WorkingDirectory(ctx) != p.want {
		p.t.Fatalf("local agent context cwd=%q want=%q", workspace.WorkingDirectory(ctx), p.want)
	}
	return p.next.Stream(ctx, req)
}
func TestLocalAgentPreparesSharedCwdWithoutRunner(t *testing.T) {
	project := t.TempDir()
	scripted, err := llm.NewScripted(llm.ScriptedConfig{Steps: []llm.ScriptedStep{{Text: "done"}}})
	if err != nil {
		t.Fatal(err)
	}
	ag, store, id := buildEmptyTestAgent(t, cwdProbeLLM{t: t, want: project, next: scripted}, nil)
	if err := store.(agentkit.SessionCwdStore).SetCwdBind(context.Background(), id, project); err != nil {
		t.Fatal(err)
	}
	ctx := rctx.WithConversation(context.Background(), string(id))
	if err := ag.RunTurn(ctx, agentkit.TurnInput{Message: agentkit.ModelMessage{Role: "user", Content: []agentkit.ContentPart{{Type: "text", Text: "hello"}}}}); err != nil {
		t.Fatal(err)
	}
}
