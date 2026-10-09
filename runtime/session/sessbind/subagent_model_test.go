package sessbind_test

import (
	"context"
	"testing"

	"github.com/lengzhao/agentkit/runtime/session/sessbind"
	rtworkspace "github.com/lengzhao/agentkit/runtime/workspace"
)

func TestResolveSubagentModel(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	ws := rtworkspace.Static(t.TempDir())
	if err := sessbind.SetGlobalModelBind(ctx, ws, "assistant", "parent-global-model"); err != nil {
		t.Fatal(err)
	}

	// 1) 定义 model 压过一切。
	if got := sessbind.ResolveSubagentModel(ctx, ws, "assistant", "sub:researcher", "from-md"); got != "from-md" {
		t.Fatalf("got %q, want from-md", got)
	}

	// 2) 无定义时用子 agent 专属 global（/model -g sub <名>）。
	if err := sessbind.SetGlobalModelBind(ctx, ws, "sub:researcher", "child-global-model"); err != nil {
		t.Fatal(err)
	}
	if got := sessbind.ResolveSubagentModel(ctx, ws, "assistant", "sub:researcher", ""); got != "child-global-model" {
		t.Fatalf("got %q, want child-global-model", got)
	}

	// 2b) wildcard 键（/model -g sub * <model>）位于子键之后、父键之前：
	// specific beats general。
	if err := sessbind.SetGlobalModelBind(ctx, ws, sessbind.SubagentModelWildcardKey, "wildcard-model"); err != nil {
		t.Fatal(err)
	}
	if got := sessbind.ResolveSubagentModel(ctx, ws, "assistant", "sub:researcher", ""); got != "child-global-model" {
		t.Fatalf("got %q, per-sub key must beat wildcard", got)
	}
	if got := sessbind.ResolveSubagentModel(ctx, ws, "assistant", "sub:vision", ""); got != "wildcard-model" {
		t.Fatalf("got %q, want wildcard-model", got)
	}

	// 3) 子键清空后 wildcard 先于父 agent 的 global。
	if err := sessbind.SetGlobalModelBind(ctx, ws, "sub:researcher", ""); err != nil {
		t.Fatal(err)
	}
	if got := sessbind.ResolveSubagentModel(ctx, ws, "assistant", "sub:researcher", ""); got != "wildcard-model" {
		t.Fatalf("got %q, want wildcard-model before parent", got)
	}

	// 3b) wildcard 清空后落到父 agent 的 global（/model -g 的一般默认）。
	if err := sessbind.SetGlobalModelBind(ctx, ws, sessbind.SubagentModelWildcardKey, ""); err != nil {
		t.Fatal(err)
	}
	if got := sessbind.ResolveSubagentModel(ctx, ws, "assistant", "sub:researcher", ""); got != "parent-global-model" {
		t.Fatalf("got %q, want parent-global-model", got)
	}

	// 4) 全空时兜底返回定义值（含空串）。
	if err := sessbind.SetGlobalModelBind(ctx, ws, "assistant", ""); err != nil {
		t.Fatal(err)
	}
	if got := sessbind.ResolveSubagentModel(ctx, ws, "assistant", "sub:researcher", ""); got != "" {
		t.Fatalf("got %q, want empty fallback", got)
	}

	// 5) ws 为 nil 时只有定义值可用。
	if got := sessbind.ResolveSubagentModel(ctx, nil, "assistant", "sub:researcher", ""); got != "" {
		t.Fatalf("got %q, want empty with nil ws", got)
	}
}
