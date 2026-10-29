package feishu

import (
	"testing"

	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/rctx"
)

func TestDMThreadMode(t *testing.T) {
	typ, root, child, topic := "p2p", "om_root", "om_child", "omt_topic"
	for _, enabled := range []bool{false, true} {
		p := &Platform{platformTag: "lark", cfg: Config{DMThreadMode: enabled}, noReplyToTrigger: true}
		msg := &larkim.EventMessage{ChatType: &typ, MessageId: &root}
		first := p.makeSessionKey(msg, "oc_c", "ou_u")
		msg.MessageId = &child
		msg.RootId = &root
		msg.ThreadId = &topic
		followup := p.makeSessionKey(msg, "oc_c", "ou_u")
		if first != followup {
			t.Fatalf("mode=%v: follow-up changed session", enabled)
		}
		if isThreadSessionKey(first) != enabled {
			t.Fatalf("mode=%v: wrong session isolation", enabled)
		}
		rc := replyContext{messageID: child, chatType: typ, sessionKey: followup}
		body := p.buildReplyMessageReqBody(rc, "text", `{"text":"hello"}`)
		threaded := body.ReplyInThread != nil && *body.ReplyInThread
		if threaded != enabled || p.shouldUseThreadOrReplyAPI(rc) != enabled {
			t.Fatalf("mode=%v: wrong reply behavior", enabled)
		}
		msg.ThreadId = nil
		second := p.makeSessionKey(msg, "oc_c", "ou_u")
		if (first != second) != enabled {
			t.Fatalf("mode=%v: wrong top-level session", enabled)
		}
		if enabled {
			raw, err := p.ReconstructReplyCtx(first)
			if err != nil || !p.shouldReplyInThread(raw.(replyContext)) {
				t.Fatal("reconstructed topic became flat", err)
			}
			if rctx.ApplyScope(agentkit.SessionID(first), agentkit.SessionScopeThread, "") == rctx.ApplyScope(agentkit.SessionID(second), agentkit.SessionScopeThread, "") {
				t.Fatal("thread scope merged topics")
			}
		}
	}
}

func TestDMThreadModePreservesGroupSettings(t *testing.T) {
	typ, id := "group", "om_1"
	p := &Platform{platformTag: "lark", cfg: Config{DMThreadMode: true}}
	msg := &larkim.EventMessage{ChatType: &typ, MessageId: &id}
	if isThreadSessionKey(p.makeSessionKey(msg, "oc_c", "ou_u")) {
		t.Fatal("DM mode enabled group isolation")
	}
	p.threadIsolation = true
	if !isThreadSessionKey(p.makeSessionKey(msg, "oc_c", "ou_u")) {
		t.Fatal("group isolation stopped working")
	}
	if p.shouldReplyInThread(replyContext{messageID: id, chatType: typ}) {
		t.Fatal("DM mode enabled group topic replies")
	}
}
