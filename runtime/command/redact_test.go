package command

import (
	"strings"
	"testing"

	"github.com/lengzhao/agentkit"
)

type sanitizerStub struct {
	stubCommand
}

func (sanitizerStub) SanitizeArgsForLog(args string) string {
	if args == "sensitive" {
		return agentkit.SlashLogRedacted
	}
	return args
}

func TestSanitizeArgsForLogUsesCommandSanitizer(t *testing.T) {
	t.Parallel()
	cmd := sanitizerStub{stubCommand{name: "custom"}}
	if sanitizeArgsForLog(cmd, "sensitive") != agentkit.SlashLogRedacted {
		t.Fatal("expected sanitizer output")
	}
	if sanitizeArgsForLog(cmd, "-u") != "-u" {
		t.Fatal("expected passthrough")
	}
}

func TestSanitizeArgsForLogWithoutSanitizer(t *testing.T) {
	t.Parallel()
	cmd := stubCommand{name: "shell"}
	if sanitizeArgsForLog(cmd, "echo secret") != "echo secret" {
		t.Fatal("expected raw args when command has no sanitizer")
	}
}

func TestSanitizeArgsForLogTruncates(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("x", 250)
	got := sanitizeArgsForLog(stubCommand{name: "ping"}, long)
	if len(got) <= 200 || !strings.HasSuffix(got, "…") {
		t.Fatalf("got len=%d, want truncated with ellipsis suffix", len(got))
	}
}
