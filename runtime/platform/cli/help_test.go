package cli_test

import (
	"bytes"
	"context"
	"io"
	"os"
	"strings"
	"testing"

	_ "github.com/lengzhao/agentkit/plugins"
	"github.com/lengzhao/agentkit/runtime/platform/cli"
)

// captureStderr swaps the process-wide os.Stderr, so its callers must not run in
// parallel — not with each other, and not with anything else in this package
// that prints. TestCLIHelpPlugin therefore stays sequential: Go resumes the
// parallel tests in cli_test.go only after the serial pass finishes.
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stderr = w
	fn()
	w.Close()
	os.Stderr = old
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

// helpOutput drives one /help invocation through Receive and returns what the
// user would have seen.
func helpOutput(t *testing.T, prompt string) string {
	t.Helper()
	p, err := cli.New(cli.Config{Prompt: prompt}, cli.Deps{})
	if err != nil {
		t.Fatal(err)
	}
	return captureStderr(t, func() {
		event, err := p.Receive(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if event.Message.Role != "" {
			t.Fatalf("a slash command must not produce a message event, got %+v", event)
		}
	})
}

// TestCLIHelpPlugin covers the wiring only; the rendering itself is tested in
// package plugindoc, which is also where doc coverage is enforced.
func TestCLIHelpPlugin(t *testing.T) {
	cases := []struct {
		name   string
		prompt string
		want   []string
	}{
		{
			name:   "list",
			prompt: "/help plugin -l",
			want:   []string{"Registered plugin kinds:", "llm/openai-compatible", "Use /help plugin <kind>"},
		},
		{
			name:   "kind",
			prompt: "/help plugin llm/openai-compatible",
			// apiKeyRef is reflected off the Config struct, not hand-written.
			want: []string{"# llm/openai-compatible", "## Config", "- apiKeyRef (string)", "## Deps"},
		},
		{
			name:   "unknown kind",
			prompt: "/help plugin no/such-kind",
			want:   []string{"unknown plugin kind"},
		},
		{
			name:   "missing kind",
			prompt: "/help plugin",
			want:   []string{"usage: /help plugin -l"},
		},
		{
			name:   "unknown topic",
			prompt: "/help nonsense",
			want:   []string{"usage: /help plugin -l"},
		},
		{
			name:   "commands",
			prompt: "/help",
			want:   []string{"Commands:", "/help plugin -l", "/exit, /quit"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := helpOutput(t, tc.prompt)
			for _, want := range tc.want {
				if !strings.Contains(out, want) {
					t.Errorf("missing %q in output:\n%s", want, out)
				}
			}
		})
	}
}
