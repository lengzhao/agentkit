package loop_test

import (
	"context"
	"strings"
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/plugins/agent/acpremote"
	"github.com/lengzhao/agentkit/runtime/command"
	rtloop "github.com/lengzhao/agentkit/runtime/loop"
	"github.com/lengzhao/agentkit/runtime/platform/common"
)

// loop/default 作为 agent 目录属主，经 CommandProvider 自贡献 /agent、/model，
// 由 commands/registry 统一收集；可选命令 deps 缺席时也不影响 turn 分发。
// 领域专属命令（/acp、/fallback、/submodel）由各自插件贡献。
func TestLoopContributesCatalogCommands(t *testing.T) {
	t.Parallel()

	inst, err := rtloop.New(rtloop.Config{DefaultAgent: "assistant"}, rtloop.Deps{
		Agents: []agentkit.Agent{stubModelAgent{id: "assistant", model: "gpt-5.4"}, stubDetailAgent{id: "worker"}},
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

func TestLoopOnlyContributesModelForCapableAgents(t *testing.T) {
	for _, tc := range []struct {
		name   string
		agents []agentkit.Agent
		want   bool
	}{
		{"ACP only", []agentkit.Agent{&acpremote.Runtime{}}, false},
		{"other agent without model configuration", []agentkit.Agent{stubDetailAgent{id: "custom"}}, false},
		{"coding agent", []agentkit.Agent{stubModelAgent{id: "coding", model: "gpt-5.4"}}, true},
		{"coding agent without configured default", []agentkit.Agent{stubModelAgent{id: "coding"}}, true},
		{"mixed agents", []agentkit.Agent{&acpremote.Runtime{}, stubModelAgent{id: "coding", model: "gpt-5.4"}}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			inst, err := rtloop.New(rtloop.Config{}, rtloop.Deps{Agents: tc.agents})
			if err != nil {
				t.Fatal(err)
			}
			registry, err := command.NewFromProviders(command.Config{}, []agentkit.CommandProvider{inst.(agentkit.CommandProvider)})
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, cmd := range registry.List(context.Background()) {
				if cmd.Name() == "model" {
					found = true
				}
			}
			if found != tc.want {
				t.Fatalf("/model registered = %v, want %v", found, tc.want)
			}
			help := common.FormatHelp(context.Background(), registry)
			if strings.Contains(help, "/model") != tc.want {
				t.Fatalf("unexpected /model visibility in help: %s", help)
			}
		})
	}
}
