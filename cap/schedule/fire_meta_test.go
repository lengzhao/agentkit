package schedule_test

import (
	"testing"

	capschedule "github.com/lengzhao/agentkit/cap/schedule"
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
		if got := capschedule.IsStatelessSessionMode(tc.mode); got != tc.want {
			t.Fatalf("mode %q: got %v want %v", tc.mode, got, tc.want)
		}
	}
}

func TestIsFireStateless(t *testing.T) {
	t.Parallel()

	if !capschedule.IsFireStateless(map[string]any{
		"schedule": map[string]any{
			"fired":       true,
			"sessionMode": "stateless",
		},
	}) {
		t.Fatal("expected stateless fire")
	}
	if capschedule.IsFireStateless(map[string]any{
		"schedule": map[string]any{
			"fired":       true,
			"sessionMode": "reuse",
		},
	}) {
		t.Fatal("expected reuse fire to keep parent history")
	}
}
