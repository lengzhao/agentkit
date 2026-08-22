package session_test

import (
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/session"
)

func TestSlackSessionID(t *testing.T) {
	t.Parallel()
	if got := session.SlackSessionID("C001", ""); got != agentkit.SessionID("slack:C001") {
		t.Fatalf("channel = %q", got)
	}
	if got := session.SlackSessionID("C001", "123.456"); got != agentkit.SessionID("slack:C001:thread:123.456") {
		t.Fatalf("thread = %q", got)
	}
}
