package session

import (
	"strings"

	"github.com/lengzhao/agentkit"
)

// FlattenTextParts joins non-empty text parts with sep (empty type is treated as text).
func FlattenTextParts(parts []agentkit.ContentPart, sep string) string {
	var b strings.Builder
	for _, p := range parts {
		if p.Type != "" && p.Type != "text" {
			continue
		}
		t := strings.TrimSpace(p.Text)
		if t == "" {
			continue
		}
		if b.Len() > 0 && sep != "" {
			b.WriteString(sep)
		}
		b.WriteString(t)
	}
	return b.String()
}
