package learning

import (
	"strings"
)

// looksLikeSecret rejects content that may contain credentials.
func looksLikeSecret(s string) bool {
	lower := strings.ToLower(s)
	needles := []string{
		"sk-", "api_key", "apikey", "secret_key", "private_key",
		"authorization: bearer", "-----begin ", "aws_secret", "password=",
	}
	for _, n := range needles {
		if strings.Contains(lower, n) {
			return true
		}
	}
	return false
}
