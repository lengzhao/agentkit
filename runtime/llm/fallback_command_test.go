package llm

import (
	"context"
	"strings"
	"testing"

	"github.com/lengzhao/agentkit"
)

func newFallbackCommand(t *testing.T, cfg FallbackConfig) agentkit.Command {
	t.Helper()
	provider, err := NewFallback(cfg, FallbackDeps{Provider: &stubProvider{}})
	if err != nil {
		t.Fatal(err)
	}
	fb, ok := provider.(*Fallback)
	if !ok {
		t.Fatalf("provider type = %T", provider)
	}
	cmds := fb.Commands()
	if len(cmds) != 1 || cmds[0].Name() != "fallback" {
		t.Fatalf("commands = %v", cmds)
	}
	if cmds[0].Alias() != "fb" {
		t.Fatalf("alias = %q", cmds[0].Alias())
	}
	return cmds[0]
}

// request-model 模式：无参用占位符展示链，带模型名展示具体链；只读。
func TestFallbackCommandRequestModelMode(t *testing.T) {
	t.Parallel()

	cmd := newFallbackCommand(t, FallbackConfig{
		FallbackModels: []string{"gpt-5.4-mini", "gpt-5.4-nano"},
	})

	out, err := cmd.CommandExec(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"fallback on: retryable",
		"fallback models: gpt-5.4-mini, gpt-5.4-nano",
		"attempt chain (primary + fallback): <request model> → gpt-5.4-mini → gpt-5.4-nano",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("out = %q, want %q", out, want)
		}
	}

	out, err = cmd.CommandExec(context.Background(), "gpt-5.4")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "primary model: gpt-5.4") ||
		!strings.Contains(out, "attempt chain (primary + fallback): gpt-5.4 → gpt-5.4-mini → gpt-5.4-nano") {
		t.Fatalf("out = %q", out)
	}

	if _, err := cmd.CommandExec(context.Background(), "set x"); err == nil {
		t.Fatal("expected usage error on extra args")
	}
}

// fixed 模式：链完全来自 config.models，与请求模型无关。
func TestFallbackCommandFixedChainMode(t *testing.T) {
	t.Parallel()

	cmd := newFallbackCommand(t, FallbackConfig{
		Models:    []string{"m1", "m2"},
		FallbackOn: "any",
	})

	out, err := cmd.CommandExec(context.Background(), "show")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "fallback on: any") ||
		!strings.Contains(out, "fixed model chain: m1 → m2") {
		t.Fatalf("out = %q", out)
	}
	if strings.Contains(out, "<request model>") {
		t.Fatalf("fixed mode must not mention request model: %q", out)
	}
}
