package common

import "strings"

// RedactToken replaces a secret token in text with [REDACTED].
func RedactToken(text, token string) string {
	if token == "" || text == "" {
		return text
	}
	return strings.ReplaceAll(text, token, "[REDACTED]")
}
