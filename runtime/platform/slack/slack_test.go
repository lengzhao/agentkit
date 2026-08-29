package slack

import "testing"

func TestNewRejectsInvalidDomain(t *testing.T) {
	_, err := New(Config{
		BotToken: "xoxb-test",
		AppToken: "xapp-test",
		Domain:   "://bad",
	}, Deps{})
	if err == nil {
		t.Fatal("expected invalid domain error")
	}
}

func TestIsDirectMessageChannel(t *testing.T) {
	if !isDirectMessageChannel("D123", "") {
		t.Fatal("D-prefixed channel should be direct")
	}
	if !isDirectMessageChannel("C123", "im") {
		t.Fatal("im channel_type should be direct")
	}
	if isDirectMessageChannel("C123", "channel") {
		t.Fatal("public channel should not be direct")
	}
}

func TestReplyThreadTS(t *testing.T) {
	if got := replyThreadTS(true, "", "1.0"); got != "" {
		t.Fatalf("dm without thread: got %q", got)
	}
	if got := replyThreadTS(true, "9.9", "1.0"); got != "9.9" {
		t.Fatalf("dm assistant thread: got %q", got)
	}
	if got := replyThreadTS(false, "", "1.0"); got != "1.0" {
		t.Fatalf("channel reply thread: got %q", got)
	}
}

func TestDeliveryReplyInThread(t *testing.T) {
	d := delivery{threadTS: "1.0", directMessage: true}
	if d.replyInThread() {
		t.Fatal("dm should not reply in thread")
	}
	d.directMessage = false
	if !d.replyInThread() {
		t.Fatal("channel should reply in thread")
	}
}

func TestIsDirectMessage(t *testing.T) {
	for _, tc := range []struct {
		channelType string
		want        bool
	}{
		{"im", true},
		{"mim", true},
		{"channel", false},
		{"group", false},
		{"", false},
	} {
		if got := isDirectMessage(tc.channelType); got != tc.want {
			t.Fatalf("channel_type=%q: got %v want %v", tc.channelType, got, tc.want)
		}
	}
}

func TestBuildSessionKey(t *testing.T) {
	p := &Platform{}
	if got := p.buildSessionKey("C1", "U1", ""); got != "slack:C1:u:U1" {
		t.Fatalf("channel message: got %q", got)
	}
	if got := p.buildSessionKey("C1", "U1", "t1"); got != "slack:C1:t:t1:u:U1" {
		t.Fatalf("thread message: got %q", got)
	}
}

func TestDeliveryThreadTS(t *testing.T) {
	if got := deliveryThreadTS(""); got != "" {
		t.Fatalf("top-level message: got %q", got)
	}
	if got := deliveryThreadTS("9.9"); got != "9.9" {
		t.Fatalf("thread message: got %q", got)
	}
}

func TestFormatSlashReplyNew(t *testing.T) {
	got := formatSlashReply("/new", "slack:C1:u:U1:new:20260829-022702.522")
	if got != "已开始新会话。" {
		t.Fatalf("got %q", got)
	}
	if got := formatSlashReply("/help", "可用命令"); got != "可用命令" {
		t.Fatalf("got %q", got)
	}
}
