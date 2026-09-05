package permission_test

import (
	"encoding/json"
	"testing"
	"time"

	capspermission "github.com/lengzhao/agentkit/cap/permission"
	rtpermission "github.com/lengzhao/agentkit/runtime/permission"
)

func TestMatchAllowDeny(t *testing.T) {
	t.Parallel()

	got := rtpermission.MatchAllowDeny(capspermission.Reply{Decision: "allow", UpdatedInput: map[string]any{"x": 1}})
	if !got.Recognized || !got.Allow || got.UpdatedInput["x"] != 1 {
		t.Fatalf("got = %+v", got)
	}

	got = rtpermission.MatchAllowDeny(capspermission.Reply{Text: "y"})
	if !got.Recognized || !got.Allow {
		t.Fatalf("text fallback = %+v", got)
	}

	got = rtpermission.MatchAllowDeny(capspermission.Reply{Decision: "deny"})
	if !got.Recognized || got.Allow {
		t.Fatalf("deny = %+v", got)
	}

	got = rtpermission.MatchAllowDeny(capspermission.Reply{Text: "maybe"})
	if got.Recognized {
		t.Fatalf("unrecognized = %+v", got)
	}
}

func TestMatchReplyIgnoresDecision(t *testing.T) {
	t.Parallel()
	got := rtpermission.MatchReply(capspermission.Reply{Decision: "allow", Text: "free"}, capspermission.Question{Prompt: "?"})
	if got.Text != "free" {
		t.Fatalf("got = %+v", got)
	}
}

func TestMatchReplyUsesSelected(t *testing.T) {
	t.Parallel()
	got := rtpermission.MatchReply(capspermission.Reply{Selected: []int{1}}, capspermission.Question{
		Options: []capspermission.Option{{Label: "alpha"}, {Label: "beta"}},
	})
	if got.Text != "beta" || len(got.Selected) != 1 || got.Selected[0] != 1 {
		t.Fatalf("got = %+v", got)
	}
}

func TestMatchReplyDoesNotParseBareNumberWithoutOptions(t *testing.T) {
	t.Parallel()
	got := rtpermission.MatchReply(capspermission.Reply{Text: "2"}, capspermission.Question{Prompt: "count?"})
	if got.Text != "2" || len(got.Selected) != 0 {
		t.Fatalf("got = %+v", got)
	}
}

func TestMatchReplyParsesNumberWithOptions(t *testing.T) {
	t.Parallel()
	got := rtpermission.MatchReply(capspermission.Reply{Text: "2"}, capspermission.Question{
		Options: []capspermission.Option{{Label: "alpha"}, {Label: "beta"}},
	})
	if got.Text != "beta" || len(got.Selected) != 1 || got.Selected[0] != 1 {
		t.Fatalf("got = %+v", got)
	}
}

func TestNoHumanAllowDeny(t *testing.T) {
	t.Parallel()
	got := rtpermission.NoHuman(capspermission.Request{Kind: capspermission.KindAllowDeny}, "headless")
	if got.Outcome != capspermission.OutcomeNoHuman || got.Allow {
		t.Fatalf("got = %+v", got)
	}
}

func TestTimedOutQuestion(t *testing.T) {
	t.Parallel()
	got := rtpermission.TimedOut(capspermission.Request{Kind: capspermission.KindQuestion})
	if got.Outcome != capspermission.OutcomeTimeout || got.Guidance == "" {
		t.Fatalf("got = %+v", got)
	}
}

func TestDecodeReply(t *testing.T) {
	t.Parallel()

	raw := rtpermission.MarshalReply(capspermission.Reply{RequestID: "perm1", Text: "yes"})
	got, err := rtpermission.DecodeReply(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got.RequestID != "perm1" || got.Text != "yes" {
		t.Fatalf("reply = %+v", got)
	}

	if _, err := rtpermission.DecodeReply(nil); err == nil {
		t.Fatal("expected empty reply to fail")
	}
	if _, err := rtpermission.DecodeReply(json.RawMessage(`{"text":"yes"}`)); err == nil {
		t.Fatal("expected missing requestId to fail")
	}
}

func TestEffectiveTimeout(t *testing.T) {
	t.Parallel()

	req := capspermission.Request{Kind: capspermission.KindAllowDeny}
	cap := capspermission.Capability{Interactive: true}

	if got := rtpermission.EffectiveTimeout(req, cap); got != capspermission.DefaultTimeout {
		t.Fatalf("interactive default = %v, want %v", got, capspermission.DefaultTimeout)
	}

	req.Timeout = 30 * time.Second
	if got := rtpermission.EffectiveTimeout(req, cap); got != 30*time.Second {
		t.Fatalf("request override = %v", got)
	}

	req.Timeout = 0
	cap.DefaultTimeout = 2 * time.Minute
	if got := rtpermission.EffectiveTimeout(req, cap); got != 2*time.Minute {
		t.Fatalf("capability override = %v", got)
	}

	cap.Interactive = false
	cap.DefaultTimeout = 0
	if got := rtpermission.EffectiveTimeout(req, cap); got != 0 {
		t.Fatalf("non-interactive = %v, want 0", got)
	}
}
