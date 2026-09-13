package session

import (
	"strings"
	"testing"

	capsessionindex "github.com/lengzhao/agentkit/cap/sessionindex"
)

func TestFormatSessionRecall(t *testing.T) {
	t.Parallel()
	got := FormatSessionRecall([]capsessionindex.Hit{
		{SessionID: "s1", Seq: 3, Role: "user", Snippet: "hello"},
	})
	if !strings.Contains(got, "s1") || !strings.Contains(got, "hello") {
		t.Fatalf("got %q", got)
	}
}
