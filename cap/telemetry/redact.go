package telemetry

import (
	"encoding/json"
	"strings"
	"unicode/utf8"
)

var sensitiveKeySubstrings = []string{
	"apikey", "api_key", "token", "password", "secret", "authorization", "credential",
}

// Usage and billing counters are not secrets; do not redact them when keys contain "token".
var nonSensitiveKeys = map[string]struct{}{
	"usage":                {},
	"inputtokens":          {},
	"outputtokens":         {},
	"totaltokens":          {},
	"prompttokens":         {},
	"completiontokens":     {},
	"usage_input_tokens":   {},
	"usage_output_tokens":  {},
	"usage_total_tokens":   {},
}

// TruncatePayload limits payload size for external export.
func TruncatePayload(raw string, maxBytes int) string {
	if maxBytes <= 0 || raw == "" {
		return raw
	}
	if len(raw) <= maxBytes {
		return raw
	}
	if maxBytes < 3 {
		return raw[:maxBytes]
	}
	cut := raw[:maxBytes-3]
	for len(cut) > 0 && !utf8.ValidString(cut) {
		cut = cut[:len(cut)-1]
	}
	return cut + "..."
}

// RedactJSON scrubs sensitive keys from JSON objects before export.
func RedactJSON(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return raw
	}
	var v any
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		return raw
	}
	redactValue(v)
	out, err := json.Marshal(v)
	if err != nil {
		return raw
	}
	return string(out)
}

func redactValue(v any) {
	switch t := v.(type) {
	case map[string]any:
		for k, val := range t {
			if isSensitiveKey(k) {
				t[k] = "[REDACTED]"
				continue
			}
			redactValue(val)
		}
	case []any:
		for i := range t {
			redactValue(t[i])
		}
	}
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

// PreparePayload applies optional redaction and truncation for exporter payloads.
func PreparePayload(raw string, maxBytes int, redact bool) string {
	if redact {
		raw = RedactJSON(raw)
	}
	return TruncatePayload(raw, maxBytes)
}
