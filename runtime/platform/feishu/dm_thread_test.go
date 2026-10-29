package feishu

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/platform/common"
	"github.com/lengzhao/agentkit/runtime/rctx"
	rtworkspace "github.com/lengzhao/agentkit/runtime/workspace"
)

func TestDMThreadAttachmentsStartTurns(t *testing.T) {
	for _, kind := range []string{"image", "file", "audio", "sticker", "media"} {
		t.Run(kind, func(t *testing.T) {
			p, srv := newCardkitMockPlatform(cardkitMockHandler(func(w http.ResponseWriter, r *http.Request) {
				if !strings.Contains(r.URL.Path, "/resources/") {
					t.Errorf("unexpected request: %s", r.URL.Path)
				}
				w.Header().Set("Content-Type", "application/octet-stream")
				_, _ = w.Write([]byte("attachment payload"))
			}))
			defer srv.Close()
			defer p.flushPendingAttachments()
			p.cfg.DMThreadMode = true
			p.inbox = common.NewInbox(1)
			p.workspace = rtworkspace.Static(t.TempDir())
			p.userProfileCache.Store("U1", feishuUserProfileEntry{})
			p.chatNameCache.Store("C1", "test")
			key := string(rctx.BuildDeliverySessionID("lark", "C1", "om_file", "U1"))
			p.dispatchMessage(context.Background(), kind, `{"file_key":"f1","image_key":"i1","file_name":"a.txt"}`, nil,
				"om_file", key, "U1", "C1", replyContext{chatID: "C1", chatType: "p2p", messageID: "om_file", sessionKey: key}, "")
			ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
			defer cancel()
			event, err := p.inbox.Receive(ctx)
			if err != nil {
				t.Fatalf("standalone DM attachment did not start a topic turn: %v", err)
			}
			if len(event.Uploads) != 1 || string(event.Uploads[0].Data) != "attachment payload" {
				t.Fatalf("attachment bytes lost: %#v", event.Uploads)
			}
			if rctx.RouteReplyTo(event.Envelope.Route) != "om_file" {
				t.Fatal("attachment turn lost its reply anchor")
			}
			if len(p.drainPendingAttachments(key)) != 0 {
				t.Fatal("DM topic attachment left in pending buffer")
			}
			// A user can continue in the topic created by the attachment reply.
			typ, root, child, topic := "p2p", "om_file", "om_followup", "omt_1"
			followup := &larkim.EventMessage{ChatType: &typ, RootId: &root, MessageId: &child, ThreadId: &topic}
			if p.makeSessionKey(followup, "C1", "U1") != key {
				t.Fatal("attachment topic follow-up selected a different session")
			}
		})
	}
}

func TestStandaloneAttachmentsKeepBufferingOutsideDMThreads(t *testing.T) {
	for _, tc := range []struct {
		name, chatType string
		dmThreads      bool
	}{{"flat DM", "p2p", false}, {"group with DM mode", "group", true}} {
		t.Run(tc.name, func(t *testing.T) {
			p, srv := newCardkitMockPlatform(cardkitMockHandler(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/octet-stream")
				_, _ = w.Write([]byte("buffered payload"))
			}))
			defer srv.Close()
			defer p.flushPendingAttachments()
			p.cfg.DMThreadMode = tc.dmThreads
			p.inbox = common.NewInbox(1)
			p.workspace = rtworkspace.Static(t.TempDir())
			p.userProfileCache.Store("U1", feishuUserProfileEntry{})
			p.chatNameCache.Store("C1", "test")
			key := string(rctx.BuildDeliverySessionID("lark", "C1", "", "U1"))
			p.dispatchMessage(context.Background(), "file", `{"file_key":"f1","file_name":"a.txt"}`, nil,
				"om_file", key, "U1", "C1", replyContext{chatID: "C1", chatType: tc.chatType, messageID: "om_file", sessionKey: key}, "")
			if saved := p.drainPendingAttachments(key); len(saved) != 1 || saved[0].OrigName != "a.txt" {
				t.Fatalf("file-first buffering changed: %#v", saved)
			}
		})
	}
}

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
