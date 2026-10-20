package acpremote

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

	acp "github.com/coder/acp-go-sdk"
)

var sensitiveDataKey = regexp.MustCompile(`(?i)(token|secret|password|api[_-]?key|auth|credential|cookie|bearer)`)

func redactValue(v any) any {
	switch x := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, val := range x {
			if sensitiveDataKey.MatchString(k) {
				out[k] = "[redacted]"
			} else {
				out[k] = redactValue(val)
			}
		}
		return out
	case map[string]string:
		out := make(map[string]any, len(x))
		for k, val := range x {
			if sensitiveDataKey.MatchString(k) {
				out[k] = "[redacted]"
			} else {
				out[k] = val
			}
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, val := range x {
			out[i] = redactValue(val)
		}
		return out
	case string:
		return redactStringValue(x)
	default:
		return v
	}
}

func redactStringValue(s string) string {
	lower := strings.ToLower(s)
	if strings.Contains(lower, "secret") || strings.Contains(lower, "token") || strings.Contains(lower, "password") {
		return "[redacted]"
	}
	return s
}

func formatRedactedRequestError(re *acp.RequestError) string {
	if re == nil {
		return ""
	}
	v := struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Data    any    `json:"data,omitempty"`
	}{
		Code:    re.Code,
		Message: re.Message,
		Data:    redactValue(re.Data),
	}
	b, err := json.Marshal(v)
	if err != nil {
		return re.Error()
	}
	return string(b)
}

// acpErrorDetail returns a single line for operators: the full error text with
// any embedded JSON-RPC payload replaced by a redacted copy.
func acpErrorDetail(err error) string {
	if err == nil {
		return ""
	}
	top := strings.TrimSpace(err.Error())
	re := requestErrorFromChain(err)
	if re == nil {
		return top
	}
	redacted := formatRedactedRequestError(re)
	raw := re.Error()
	if strings.Contains(top, raw) {
		return strings.Replace(top, raw, redacted, 1)
	}
	if top != "" && top != raw {
		return top + ": " + redacted
	}
	return redacted
}

func requestErrorFromChain(err error) *acp.RequestError {
	for e := err; e != nil; {
		var re *acp.RequestError
		if errors.As(e, &re) && re != nil {
			return re
		}
		e = errors.Unwrap(e)
	}
	return nil
}

func acpErrorStage(err error) string {
	if err == nil {
		return ""
	}
	lower := strings.ToLower(err.Error())
	switch {
	case strings.Contains(lower, "authenticate"):
		return "authenticate"
	case strings.Contains(lower, "initialize"):
		return "initialize"
	case strings.Contains(lower, "session/new"):
		return "session/new"
	case strings.Contains(lower, "session/resume"):
		return "session/resume"
	case strings.Contains(lower, "session/load"):
		return "session/load"
	case strings.Contains(lower, "session/prompt"), strings.Contains(lower, "prompt"):
		return "session/prompt"
	default:
		return "turn"
	}
}

func wrapACPStageError(stage string, err error) error {
	if err == nil || stage == "" {
		return err
	}
	lower := strings.ToLower(err.Error())
	tag := "acp " + stage
	if strings.Contains(lower, tag) {
		return err
	}
	return fmt.Errorf("%s: %w", tag, err)
}
