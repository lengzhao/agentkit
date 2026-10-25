package chatapi

import (
	"context"
	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/platform/common"
	"github.com/lengzhao/agentkit/runtime/platform/multiplex"
	"github.com/lengzhao/agentkit/runtime/rctx"
	"testing"
)

func TestChatAPIStorageAndSlashKeepInstance(t *testing.T) {
	p := &Platform{}
	if _, err := multiplex.New(multiplex.Config{}, multiplex.Deps{Platforms: []agentkit.Platform{&Platform{}, p}}); err != nil {
		t.Fatal(err)
	}
	ctx := p.sessionCtx(context.Background(), "C1", "conv")
	if got := rctx.ConversationFromContext(ctx); got != "chat-api1:C1:t:conv" {
		t.Fatalf("conversation=%q", got)
	}
	slash := common.SlashCommandContext(context.Background(), nil, p.slashContext("chat-api:C1:t:conv", nil, "U1", nil))
	if got := rctx.ConversationFromContext(slash); got != "chat-api1:C1:t:conv" {
		t.Fatalf("slash=%q", got)
	}
	if got := rctx.WorkspaceFromContext(ctx); got != "chat-api1:C1" {
		t.Fatalf("workspace=%q", got)
	}
}
