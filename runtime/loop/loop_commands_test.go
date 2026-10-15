package loop_test

import (
	"context"
	"strings"
	"testing"

	"github.com/lengzhao/agentkit"
	rtloop "github.com/lengzhao/agentkit/runtime/loop"
)

// loop/default 作为 agent 目录属主，经 CommandProvider 自贡献 /agent、/model，
// 由 commands/registry 统一收集；可选命令 deps 缺席时也不影响 turn 分发。
// 领域专属命令（/acp、/fallback、/submodel）由各自插件贡献。
func TestLoopContributesCatalogCommands(t *testing.T) {
	t.Parallel()

	inst, err := rtloop.New(rtloop.Config{DefaultAgent: "assistant"}, rtloop.Deps{
		Agents: []agentkit.Agent{stubDetailAgent{id: "assistant"}, stubDetailAgent{id: "worker"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	provider, ok := inst.(agentkit.CommandProvider)
	if !ok {
		t.Fatal("loop must implement CommandProvider")
	}

	byName := map[string]agentkit.Command{}
	for _, cmd := range provider.Commands() {
		byName[cmd.Name()] = cmd
	}
	for _, name := range []string{"agent", "model"} {
		if byName[name] == nil {
			t.Fatalf("missing /%s command", name)
		}
	}

	out, err := byName["agent"].CommandExec(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "assistant") || !strings.Contains(out, "worker") {
		t.Fatalf("out = %q, want registered agents listed", out)
	}
}
