package permission

import "testing"

func TestMatchAllowDeny(t *testing.T) {
	t.Parallel()

	got := MatchAllowDeny(Reply{Decision: "allow", UpdatedInput: map[string]any{"x": 1}})
	if !got.Recognized || !got.Allow || got.UpdatedInput["x"] != 1 {
		t.Fatalf("got = %+v", got)
	}

	got = MatchAllowDeny(Reply{Text: "y"})
	if !got.Recognized || !got.Allow {
		t.Fatalf("text fallback = %+v", got)
	}

	got = MatchAllowDeny(Reply{Decision: "deny"})
	if !got.Recognized || got.Allow {
		t.Fatalf("deny = %+v", got)
	}

	got = MatchAllowDeny(Reply{Text: "maybe"})
	if got.Recognized {
		t.Fatalf("unrecognized = %+v", got)
	}
}

func TestMatchReplyIgnoresDecision(t *testing.T) {
	t.Parallel()
	got := MatchReply(Reply{Decision: "allow", Text: "free"}, Question{Prompt: "?"})
	if got.Text != "free" {
		t.Fatalf("got = %+v", got)
	}
}

func TestMatchReplyUsesSelected(t *testing.T) {
	t.Parallel()
	got := MatchReply(Reply{Selected: []int{1}}, Question{
		Options: []Option{{Label: "alpha"}, {Label: "beta"}},
	})
	if got.Text != "beta" || len(got.Selected) != 1 || got.Selected[0] != 1 {
		t.Fatalf("got = %+v", got)
	}
}

func TestMatchReplyDoesNotParseBareNumberWithoutOptions(t *testing.T) {
	t.Parallel()
	got := MatchReply(Reply{Text: "2"}, Question{Prompt: "count?"})
	if got.Text != "2" || len(got.Selected) != 0 {
		t.Fatalf("got = %+v", got)
	}
}

func TestMatchReplyParsesNumberWithOptions(t *testing.T) {
	t.Parallel()
	got := MatchReply(Reply{Text: "2"}, Question{
		Options: []Option{{Label: "alpha"}, {Label: "beta"}},
	})
	if got.Text != "beta" || len(got.Selected) != 1 || got.Selected[0] != 1 {
		t.Fatalf("got = %+v", got)
	}
}

func TestNoHumanAllowDeny(t *testing.T) {
	t.Parallel()
	got := NoHuman(Request{Kind: KindAllowDeny}, "headless")
	if got.Outcome != OutcomeNoHuman || got.Allow {
		t.Fatalf("got = %+v", got)
	}
}

func TestTimedOutQuestion(t *testing.T) {
	t.Parallel()
	got := TimedOut(Request{Kind: KindQuestion})
	if got.Outcome != OutcomeTimeout || got.Guidance == "" {
		t.Fatalf("got = %+v", got)
	}
}
