package feishu

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/rctx"
)

type dmTestWorkspace string

func (w dmTestWorkspace) Resolve(_ context.Context, rel string) (string, error) {
	return filepath.Join(string(w), rel), nil
}

func TestDMThreadRouting(t *testing.T) {
	p := &Platform{platformTag: "lark", cfg: Config{DMThreadMode: true}, workspace: dmTestWorkspace(t.TempDir())}
	chatType, root, child, thread := "p2p", "om_root", "om_child", "omt_thread"
	msg := &larkim.EventMessage{ChatType: &chatType, MessageId: &root}
	key := p.makeSessionKey(msg, "oc_chat", "ou_user")
	if got := rctx.ParseDelivery(agentkit.SessionID(key), "").Thread; got != root {
		t.Fatalf("root thread = %q", got)
	}
	msg.MessageId = &child
	msg.RootId = &root
	msg.ThreadId = &thread
	if got := p.makeSessionKey(msg, "oc_chat", "ou_user"); got != key {
		t.Fatalf("reply routed to %s, want %s", got, key)
	}
	p.cfg.DMThreadMode = false
	if got := p.makeSessionKey(msg, "oc_chat", "ou_user"); got != key {
		t.Fatal("existing topic lost after mode disabled")
	}
	rc := replyContext{messageID: child, chatType: chatType, sessionKey: key}
	if body := p.buildReplyMessageReqBody(rc, "text", `{"text":"reply"}`); body.ReplyInThread == nil || !*body.ReplyInThread {
		t.Fatal("DM topic reply is flat")
	}
	if !p.shouldUseThreadOrReplyAPI(rc) {
		t.Fatal("topic must use reply API")
	}
	p.noReplyToTrigger = true
	if !p.shouldUseThreadOrReplyAPI(rc) {
		t.Fatal("replyToTrigger=false must not flatten DM topic")
	}
}

func TestDMThreadPreferences(t *testing.T) {
	ws := dmTestWorkspace(t.TempDir())
	p := &Platform{platformTag: "lark", workspace: ws}
	if p.dmThreadEnabled("u1") {
		t.Fatal("default must be flat")
	}
	handled, reply := p.dmThreadCommand(context.Background(), "u1", "/dm-thread on")
	if !handled || reply == "" || !p.dmThreadEnabled("u1") {
		t.Fatal("enable failed", reply)
	}
	if p.dmThreadEnabled("u2") {
		t.Fatal("preference leaked to another user")
	}
	restarted := &Platform{platformTag: "lark", workspace: ws}
	if !restarted.dmThreadEnabled("u1") {
		t.Fatal("preference not persisted")
	}
	restarted.SetPlatformInstance("other")
	if restarted.dmThreadEnabled("u1") {
		t.Fatal("preference leaked to another instance")
	}
	handled, _ = p.dmThreadCommand(context.Background(), "u1", "/dm-thread off")
	if !handled || p.dmThreadEnabled("u1") {
		t.Fatal("disable failed")
	}
	if handled, _ := p.dmThreadCommand(context.Background(), "u1", "/dm-thread on extra"); !handled || p.dmThreadEnabled("u1") {
		t.Fatal("invalid command changed mode")
	}
}

func TestDMThreadFlatAndGroupCompatibility(t *testing.T) {
	p := &Platform{platformTag: "lark", workspace: dmTestWorkspace(t.TempDir())}
	typ, root, other := "p2p", "om_root", "om_other"
	msg := &larkim.EventMessage{ChatType: &typ, MessageId: &root, RootId: &other}
	flat := p.makeSessionKey(msg, "oc_chat", "ou_user")
	if isThreadSessionKey(flat) {
		t.Fatal("flat quoted DM unexpectedly isolated")
	}
	p.cfg.DMThreadMode = true
	msg.RootId = nil
	first := p.makeSessionKey(msg, "oc_chat", "ou_user")
	msg.MessageId = &other
	second := p.makeSessionKey(msg, "oc_chat", "ou_user")
	if first == second {
		t.Fatal("independent topics share delivery")
	}
	if rctx.ApplyScope(agentkit.SessionID(first), agentkit.SessionScopeThread, "") == rctx.ApplyScope(agentkit.SessionID(second), agentkit.SessionScopeThread, "") {
		t.Fatal("thread scope merged conversations")
	}
	typ = "group"
	if isThreadSessionKey(p.makeSessionKey(msg, "oc_chat", "ou_user")) {
		t.Fatal("DM preference changed group isolation")
	}
	p.threadIsolation = true
	if !isThreadSessionKey(p.makeSessionKey(msg, "oc_chat", "ou_user")) {
		t.Fatal("group isolation stopped working")
	}
}

func TestDMThreadPreferenceWriteFailure(t *testing.T) {
	p := &Platform{platformTag: "lark"}
	handled, reply := p.dmThreadCommand(context.Background(), "u1", "/dm-thread on")
	if !handled || p.dmThreadEnabled("u1") || !strings.Contains(reply, "失败") {
		t.Fatalf("write failure reported success: %s", reply)
	}
}

func TestDMThreadQuotedTopLevelStartsNewTopic(t *testing.T) {
	p := &Platform{platformTag: "lark", cfg: Config{DMThreadMode: true}}
	typ, id, quote := "p2p", "om_new", "om_old"
	msg := &larkim.EventMessage{ChatType: &typ, MessageId: &id, RootId: &quote}
	key := p.makeSessionKey(msg, "oc_chat", "ou_user")
	if got := rctx.ParseDelivery(agentkit.SessionID(key), "").Thread; got != id {
		t.Fatalf("new top-level message joined quote topic %s", got)
	}
}

func TestDMThreadReconstructedDelivery(t *testing.T) {
	p := &Platform{platformTag: "lark", noReplyToTrigger: true}
	raw, err := p.ReconstructReplyCtx("lark:oc_c:t:om_root:u:ou_u")
	if err != nil {
		t.Fatal(err)
	}
	rc := raw.(replyContext)
	if !p.shouldReplyInThread(rc) || !p.shouldUseThreadOrReplyAPI(rc) {
		t.Fatal("reconstructed explicit topic route became flat")
	}
}

func TestDMThreadQuoteContext(t *testing.T) {
	p := &Platform{}
	root := replyContext{chatType: "p2p", messageID: "om_root", sessionKey: "lark:oc_c:t:om_root:u:ou_u"}
	if !p.shouldFetchQuotedMessage(root, "om_quoted") {
		t.Fatal("new topic lost its quoted context")
	}
	root.messageID = "om_child"
	if p.shouldFetchQuotedMessage(root, "om_root") {
		t.Fatal("topic follow-up duplicates existing context")
	}
}
