package feishu

import (
	"context"
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/platform/common"
	"github.com/lengzhao/agentkit/runtime/platform/multiplex"
	"github.com/lengzhao/agentkit/runtime/rctx"
)

type instanceCommands struct{ conversation string }

func (c *instanceCommands) List(context.Context) []agentkit.Command { return nil }
func (c *instanceCommands) Dispatch(ctx context.Context, _ string, _ string) (string, error) {
	c.conversation = rctx.ConversationFromContext(ctx)
	return "", nil
}
func TestNativeCommandAndMessageShareInstance(t *testing.T) {
	c := &instanceCommands{}
	p := &Platform{commands: c, inbox: common.NewInbox(2), platformTag: "lark"}
	first := &Platform{}
	first.platformTag = "lark"
	if _, err := multiplex.New(multiplex.Config{}, multiplex.Deps{Platforms: []agentkit.Platform{first, p}}); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for _, text := range []string{"/new", "hello"} {
		p.dispatchInbound(ctx, inboundMessage{sessionID: "lark:C1", userID: "U1", content: text, rctx: replyContext{chatID: "C1"}})
	}
	event, err := p.inbox.Receive(ctx)
	if err != nil {
		t.Fatal(err)
	}
	env := rctx.ResolveEnvelope(event, rctx.DefaultRoutePolicy(agentkit.SessionScopeChannel))
	if c.conversation != "lark1:C1" || env.Conversation != c.conversation {
		t.Fatalf("command=%q normal=%q", c.conversation, env.Conversation)
	}
}
