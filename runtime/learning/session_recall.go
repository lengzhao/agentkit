package learning

import (
	"fmt"
	"strings"

	capsessionindex "github.com/lengzhao/agentkit/cap/sessionindex"
)

// FormatSessionRecall renders FTS hits for the review digest.
func FormatSessionRecall(hits []capsessionindex.Hit) string {
	if len(hits) == 0 {
		return ""
	}
	var b strings.Builder
	for i, h := range hits {
		if i > 0 {
			b.WriteByte('\n')
		}
		fmt.Fprintf(&b, "- session=%s seq=%d role=%s: %s", h.SessionID, h.Seq, h.Role, strings.TrimSpace(h.Snippet))
	}
	return b.String()
}
