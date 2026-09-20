package schedule_test

import (
	"testing"

	rtschedule "github.com/lengzhao/agentkit/runtime/schedule"
)

func TestIsStatelessSessionMode(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		mode string
		want bool
	}{
		{"", true},
		{"stateless", true},
		{"fresh", true},
		{"reuse", false},
		{"fixed", false},
	} {
		if got := rtschedule.IsStatelessSessionMode(tc.mode); got != tc.want {
			t.Fatalf("mode %q: got %v want %v", tc.mode, got, tc.want)
		}
	}
}

func TestIsFireStateless(t *testing.T) {
	t.Parallel()

	if !rtschedule.IsFireStateless(map[string]any{
		"schedule": map[string]any{
			"fired":       true,
			"sessionMode": "stateless",
		},
	}) {
		t.Fatal("expected stateless fire")
	}
	if rtschedule.IsFireStateless(map[string]any{
		"schedule": map[string]any{
			"fired":       true,
			"sessionMode": "reuse",
		},
	}) {
		t.Fatal("expected reuse fire to be stateful")
	}
}
