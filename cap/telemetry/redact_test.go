package telemetry_test

import (
	"testing"

	"github.com/lengzhao/agentkit/cap/telemetry"
)

func TestRedactJSONScrubsSensitiveKeys(t *testing.T) {
	t.Parallel()
	raw := `{"apiKey":"secret","nested":{"password":"x"},"safe":"ok"}`
	got := telemetry.RedactJSON(raw)
	if got == raw {
		t.Fatalf("expected redaction, got %q", got)
	}
	if contains(got, "secret") || contains(got, `"x"`) {
		t.Fatalf("sensitive values leaked: %s", got)
	}
}

func TestPreparePayloadTruncates(t *testing.T) {
	t.Parallel()
	got := telemetry.PreparePayload("abcdefghijklmnopqrstuvwxyz", 10, false)
	if len(got) > 10 {
		t.Fatalf("expected truncation, got len=%d %q", len(got), got)
	}
}

func contains(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
