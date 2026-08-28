package session_test

import (
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/session"
)

func TestApplyScope(t *testing.T) {
	t.Parallel()

	delivery := session.BuildDeliverySessionID("slack", "C001", "123.456", "U777")

	cases := []struct {
		name  string
		scope session.SessionScope
		want  agentkit.SessionID
	}{
		{"channel", session.ScopeChannel, "slack:C001"},
		{"thread", session.ScopeThread, "slack:C001:t:123.456"},
		{"user", session.ScopeUser, "slack:C001:u:U777"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := session.ApplyScope(delivery, tc.scope, "U777")
			if got != tc.want {
				t.Fatalf("ApplyScope(%q, %q) = %q, want %q", delivery, tc.scope, got, tc.want)
			}
		})
	}
}

func TestApplyScopeLegacySlackUserDelivery(t *testing.T) {
	t.Parallel()

	got := session.ApplyScope("slack:C001:U777", session.ScopeUser, "")
	if got != agentkit.SessionID("slack:C001:u:U777") {
		t.Fatalf("got %q", got)
	}
}

func TestApplyScopePassthroughCLI(t *testing.T) {
	t.Parallel()

	id := agentkit.SessionID("cli:default")
	for _, scope := range []session.SessionScope{session.ScopeChannel, session.ScopeThread, session.ScopeUser} {
		if got := session.ApplyScope(id, scope, ""); got != id {
			t.Fatalf("scope %q changed cli id to %q", scope, got)
		}
	}
}

func TestParseScopeDefaultsToChannel(t *testing.T) {
	t.Parallel()

	for _, raw := range []string{"", "channel", "CHANNEL", "bogus"} {
		if raw == "bogus" {
			if got := session.ParseScope(raw); got != session.ScopeChannel {
				t.Fatalf("ParseScope(%q) = %q, want channel", raw, got)
			}
			continue
		}
		if got := session.ParseScope(raw); got != session.ScopeChannel {
			t.Fatalf("ParseScope(%q) = %q, want channel", raw, got)
		}
	}
	if got := session.ParseScope("thread"); got != session.ScopeThread {
		t.Fatalf("got %q", got)
	}
	if got := session.ParseScope("user"); got != session.ScopeUser {
		t.Fatalf("got %q", got)
	}
}

func TestBuildDeliverySessionID(t *testing.T) {
	t.Parallel()

	got := session.BuildDeliverySessionID("slack", "C001", "123.456", "U777")
	want := agentkit.SessionID("slack:C001:t:123.456:u:U777")
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestDeliveryWithUser(t *testing.T) {
	t.Parallel()

	delivery := session.BuildDeliverySessionID("slack", "C001", "123.456", "U111")
	got := session.DeliveryWithUser(delivery, "U222")
	want := session.BuildDeliverySessionID("slack", "C001", "123.456", "U222")
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}
