package feishu

import "testing"

func TestShouldReplyInThread(t *testing.T) {
	t.Parallel()

	p := &Platform{replyInThread: true}
	group := replyContext{messageID: "om_1", chatType: "group"}

	if !p.shouldReplyInThread(group) {
		t.Fatal("expected true for group message")
	}
	if p.shouldReplyInThread(replyContext{messageID: "om_1"}) {
		t.Fatal("expected false when chatType is empty")
	}
	if p.shouldReplyInThread(replyContext{messageID: "om_1", chatType: "p2p"}) {
		t.Fatal("expected false for p2p message")
	}
	if p.shouldReplyInThread(replyContext{chatType: "group"}) {
		t.Fatal("expected false without message id")
	}

	p.replyInThread = false
	if p.shouldReplyInThread(group) {
		t.Fatal("expected false when replyInThread disabled")
	}
}
