package session

import (
	"strings"

	"github.com/lengzhao/agentkit"
)

// FlattenTextParts joins non-empty text parts with sep (empty type is treated as text).
func FlattenTextParts(parts []agentkit.ContentPart, sep string) string {
	var b strings.Builder
	for _, part := range parts {
		if part.Type != "" && part.Type != "text" {
			continue
		}
		text := strings.TrimSpace(part.Text)
		if text == "" {
			continue
		}
		if b.Len() > 0 {
			b.WriteString(sep)
		}
		b.WriteString(text)
	}
	return b.String()
}
