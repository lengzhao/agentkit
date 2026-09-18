package feishu

import (
	"context"
	"strings"

	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
)

// sanitizingLogger wraps a logger and masks sensitive URL parameters.
type sanitizingLogger struct {
	inner larkcore.Logger
}

func (l *sanitizingLogger) maskURL(args ...interface{}) []interface{} {
	masked := make([]interface{}, len(args))
	for i, arg := range args {
		if s, ok := arg.(string); ok {
			masked[i] = l.sanitize(s)
		} else {
			masked[i] = arg
		}
	}
	return masked
}

func (l *sanitizingLogger) sanitize(s string) string {
	// Mask sensitive query parameters in URLs
	sensitiveParams := []string{
		"device_id=", "access_key=", "ticket=", "conn_id=",
		"secret=", "token=", "password=", "key=",
	}
	for _, param := range sensitiveParams {
		if idx := strings.Index(s, param); idx != -1 {
			// Find the end of the value (either & or end of string)
			end := idx + len(param)
			for end < len(s) && s[end] != '&' && s[end] != ' ' {
				end++
			}
			s = s[:idx+len(param)] + "***" + s[end:]
		}
	}
	return s
}

func (l *sanitizingLogger) Debug(ctx context.Context, args ...interface{}) {
	for _, arg := range args {
		s, ok := arg.(string)
		if !ok {
			continue
		}
		msg := strings.ToLower(s)
		if strings.Contains(msg, "ping success") || strings.Contains(msg, "receive pong") {
			return
		}
	}
	l.inner.Debug(ctx, l.maskURL(args...)...)
}

func (l *sanitizingLogger) Info(ctx context.Context, args ...interface{}) {
	l.inner.Info(ctx, l.maskURL(args...)...)
}

func (l *sanitizingLogger) Warn(ctx context.Context, args ...interface{}) {
	l.inner.Warn(ctx, l.maskURL(args...)...)
}

func (l *sanitizingLogger) Error(ctx context.Context, args ...interface{}) {
	l.inner.Error(ctx, l.maskURL(args...)...)
}
