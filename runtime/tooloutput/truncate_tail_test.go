package tooloutput

import (
	"strings"
	"testing"
)

func TestTruncateTailWithinLimits(t *testing.T) {
	in := "line1\nline2\n"
	got := TruncateTail(in, DefaultMaxLines, DefaultMaxBytes)
	if got.Truncated || got.Content != in {
		t.Fatalf("got %#v", got)
	}
}

func TestTruncateTailByLines(t *testing.T) {
	lines := make([]string, 5)
	for i := range lines {
		lines[i] = "x"
	}
	in := strings.Join(lines, "\n")
	got := TruncateTail(in, 2, DefaultMaxBytes)
	if !got.Truncated || got.TruncatedBy != "lines" {
		t.Fatalf("got %#v", got)
	}
	if got.OutputLines != 2 {
		t.Fatalf("output lines = %d", got.OutputLines)
	}
}
