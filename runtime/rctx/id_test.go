package rctx_test

import (
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/rctx"
)

func TestSlackSessionID(t *testing.T) {
	t.Parallel()
	if got := rctx.SlackSessionID("C001", ""); got != agentkit.SessionID("slack:C001") {
		t.Fatalf("channel = %q", got)
	}
	if got := rctx.SlackSessionID("C001", "123.456"); got != agentkit.SessionID("slack:C001:t:123.456") {
		t.Fatalf("thread = %q", got)
	}
}

func TestSlackSessionIDForScope(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		scope agentkit.SessionScope
		want  agentkit.SessionID
	}{
		{"channel shares one session", agentkit.SessionScopeChannel, "slack:C001"},
		{"thread splits per thread", agentkit.SessionScopeThread, "slack:C001:t:123.456"},
		{"user splits per person", agentkit.SessionScopeUser, "slack:C001:u:U777"},
		{"unknown scope falls back to channel", agentkit.SessionScope("bogus"), "slack:C001"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := rctx.SlackSessionIDForScope(tc.scope, "C001", "123.456", "U777")
			if got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
}

// Session granularity and workdir granularity are independent: however finely a
// channel's history is split, every scope must land on the channel's tenant.
func TestEverySlackScopeSharesOneTenant(t *testing.T) {
	t.Parallel()

	scopes := []agentkit.SessionScope{agentkit.SessionScopeChannel, agentkit.SessionScopeThread, agentkit.SessionScopeUser}
	for _, scope := range scopes {
		id := rctx.SlackSessionIDForScope(scope, "C001", "123.456", "U777")
		if key := rctx.WorkspaceKey(string(id)); key != "slack:C001" {
			t.Fatalf("scope %q: session %q -> tenant %q, want slack:C001", scope, id, key)
		}
	}
}

func TestScopeUserWithoutUserFallsBackToChannel(t *testing.T) {
	t.Parallel()
	got := rctx.SlackSessionIDForScope(agentkit.SessionScopeUser, "C001", "", "")
	if got != agentkit.SessionID("slack:C001") {
		t.Fatalf("got %q", got)
	}
}
