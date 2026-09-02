package config

import "strings"

var sensitiveKeySubstrings = []string{
	"apikey", "api_key", "token", "password", "secret", "authorization",
}

// nonSensitiveKeys excludes usage counters that happen to contain "token".
var nonSensitiveKeys = map[string]struct{}{
	"usage":               {},
	"inputtokens":         {},
	"outputtokens":        {},
	"totaltokens":         {},
	"prompttokens":        {},
	"completiontokens":    {},
	"usage_input_tokens":  {},
	"usage_output_tokens": {},
	"usage_total_tokens":  {},
}

// RedactInstances scrubs resolved config values that look like secrets.
// Credential refs such as env:OPENAI_API_KEY are kept so dumps stay debuggable.
func RedactInstances(raw map[string]any) {
	redactValue(raw)
}

func redactValue(v any) {
	switch t := v.(type) {
	case map[string]any:
		for k, val := range t {
			if shouldRedactField(k, val) {
				t[k] = "[REDACTED]"
				continue
			}
			redactValue(val)
		}
	case []any:
		for i := range t {
			if !shouldRedactField("", t[i]) {
				redactValue(t[i])
			}
		}
	}
}

func shouldRedactField(key string, val any) bool {
	str, ok := val.(string)
	if !ok || str == "" {
		return false
	}
	if strings.HasSuffix(strings.ToLower(strings.TrimSpace(key)), "ref") {
		return false
	}
	if isCredentialRef(str) {
		return false
	}
	return isSensitiveKey(key)
}

func isCredentialRef(s string) bool {
	s = strings.TrimSpace(s)
	return strings.HasPrefix(s, "env:") || strings.HasPrefix(s, "file:")
}

func isSensitiveKey(key string) bool {
	lower := strings.ToLower(strings.TrimSpace(key))
	if _, ok := nonSensitiveKeys[lower]; ok {
		return false
	}
	for _, sub := range sensitiveKeySubstrings {
		if strings.Contains(lower, sub) {
			return true
		}
	}
	return false
}
