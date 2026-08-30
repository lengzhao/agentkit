package cli

import (
	"strings"
	"testing"
)

func TestInputReadPromptMultipleLines(t *testing.T) {
	t.Parallel()

	in := NewInput(strings.NewReader("first\n/new\nsecond\n"))
	got1, err := in.ReadPrompt()
	if err != nil || got1 != "first" {
		t.Fatalf("line 1 = %q err=%v", got1, err)
	}
	got2, err := in.ReadPrompt()
	if err != nil || got2 != "/new" {
		t.Fatalf("line 2 = %q err=%v", got2, err)
	}
	got3, err := in.ReadPrompt()
	if err != nil || got3 != "second" {
		t.Fatalf("line 3 = %q err=%v", got3, err)
	}
}
