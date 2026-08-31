package runner

import (
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/session"
)

// Built-in inject tokens for runner.config.inject (cc-connect inject_* style).
//
//	sender_id sender_name sender_email platform chat_id timestamp
//	task_id trace_id language
//	custom.*   — metadata keys with prefix custom.
//	<any-key>  — exact MessageEvent.Metadata lookup
const (
	injectSenderID    = "sender_id"
	injectSenderName  = "sender_name"
	injectSenderEmail = "sender_email"
	injectPlatform    = "platform"
	injectChatID      = "chat_id"
	injectTimestamp   = "timestamp"
	injectTaskID      = "task_id"
	injectTraceID     = "trace_id"
	injectLanguage    = "language"
)

type inboundFormatConfig struct {
	inject          []string
	defaultTimezone string
}

func (r *Root) formatInboundEvent(event agentkit.MessageEvent, deliveryID agentkit.SessionID) agentkit.MessageEvent {
	if event.Message.Role == "" || skipInboundPromptMeta(event.Metadata) {
		return event
	}
	prefix := r.buildInboundPromptPrefix(event, deliveryID)
	if prefix == "" {
		return event
	}
	event.Message = prependInboundPromptPrefix(event.Message, prefix)
	return event
}

func skipInboundPromptMeta(metadata map[string]any) bool {
	if len(metadata) == 0 {
		return false
	}
	raw, ok := metadata[agentkit.MetadataSkipPromptMeta]
	if !ok {
		return false
	}
	switch v := raw.(type) {
	case bool:
		return v
	case string:
		return strings.EqualFold(strings.TrimSpace(v), "true")
	default:
		return false
	}
}

func (r *Root) buildInboundPromptPrefix(event agentkit.MessageEvent, deliveryID agentkit.SessionID) string {
	cfg := inboundFormatConfig{
		inject:          r.inject,
		defaultTimezone: r.defaultTimezone,
	}
	return buildInboundPromptPrefix(cfg, event, deliveryID, r.platform, time.Now())
}

func buildInboundPromptPrefix(cfg inboundFormatConfig, event agentkit.MessageEvent, deliveryID agentkit.SessionID, platform agentkit.Platform, now time.Time) string {
	attrs := injectPromptAttrs(cfg, event, deliveryID, platform, now)
	if len(attrs) == 0 {
		return ""
	}
	return fmt.Sprintf("[agentkit %s]", strings.Join(attrs, " "))
}

func injectPromptAttrs(cfg inboundFormatConfig, event agentkit.MessageEvent, deliveryID agentkit.SessionID, platform agentkit.Platform, now time.Time) []string {
	if len(cfg.inject) == 0 {
		return nil
	}
	parts := session.ParseDelivery(deliveryID, event.UserID)
	platformID := strings.TrimSpace(event.PlatformID)
	if platformID == "" {
		platformID = parts.Platform
	}

	var attrs []string
	for _, item := range cfg.inject {
		key := strings.ToLower(strings.TrimSpace(item))
		if key == "" {
			continue
		}
		switch key {
		case injectSenderID:
			if id := strings.TrimSpace(event.UserID); id != "" {
				attrs = append(attrs, fmt.Sprintf("sender_id=%s", id))
			}
		case injectSenderName:
			if name := senderNameFromMetadata(event.Metadata); name != "" {
				attrs = append(attrs, fmt.Sprintf(`sender_name="%s"`, promptAttrValue(name)))
			}
		case injectSenderEmail:
			if email := senderEmailFromMetadata(event.Metadata); email != "" {
				attrs = append(attrs, fmt.Sprintf(`sender_email="%s"`, promptAttrValue(email)))
			}
		case injectPlatform:
			if platformID != "" {
				attrs = append(attrs, fmt.Sprintf("platform=%s", platformID))
			}
		case injectChatID:
			if parts.Channel != "" {
				attrs = append(attrs, fmt.Sprintf("chat_id=%s", parts.Channel))
			}
		case injectTimestamp:
			tz := resolveUserTimezone(cfg.defaultTimezone, event.UserID, platform)
			attrs = append(attrs, formatInjectTimestamp(now, tz))
		case injectTaskID:
			if v := contextValueFromMetadata(event.Metadata, injectTaskID, "taskId", "X-Task-Id", "X-Task-ID"); v != "" {
				attrs = append(attrs, fmt.Sprintf(`task_id="%s"`, promptAttrValue(v)))
			}
		case injectTraceID:
			if v := contextValueFromMetadata(event.Metadata, injectTraceID, "traceId", "X-Trace-Id", "X-Trace-ID"); v != "" {
				attrs = append(attrs, fmt.Sprintf(`trace_id="%s"`, promptAttrValue(v)))
			}
		case injectLanguage:
			if v := contextValueFromMetadata(event.Metadata, injectLanguage, "lang", "X-Language"); v != "" {
				attrs = append(attrs, fmt.Sprintf(`language="%s"`, promptAttrValue(v)))
			}
		default:
			if strings.HasSuffix(key, ".*") {
				attrs = append(attrs, metadataPromptAttrs(event.Metadata, []string{item})...)
				continue
			}
			if v := metadataString(event.Metadata, item); v != "" {
				attrs = append(attrs, fmt.Sprintf(`%s="%s"`, strings.TrimSpace(item), promptAttrValue(v)))
			}
		}
	}
	return attrs
}

func normalizeInjectAllowlist(keys []string) []string {
	if len(keys) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(keys))
	out := make([]string, 0, len(keys))
	for _, key := range keys {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		canon := strings.ToLower(key)
		if _, ok := seen[canon]; ok {
			continue
		}
		if !isKnownInjectToken(canon) && !strings.HasSuffix(canon, ".*") {
			slog.Debug("runner: inject entry treated as metadata key", "key", key)
		}
		seen[canon] = struct{}{}
		out = append(out, key)
	}
	return out
}

func isKnownInjectToken(key string) bool {
	switch key {
	case injectSenderID, injectSenderName, injectSenderEmail, injectPlatform, injectChatID,
		injectTimestamp, injectTaskID, injectTraceID, injectLanguage:
		return true
	default:
		return strings.HasSuffix(key, ".*")
	}
}

func prependInboundPromptPrefix(msg agentkit.ModelMessage, prefix string) agentkit.ModelMessage {
	if prefix == "" {
		return msg
	}
	content := append([]agentkit.ContentPart(nil), msg.Content...)
	for i, part := range content {
		if part.Type != "text" {
			continue
		}
		part.Text = prefix + "\n" + part.Text
		content[i] = part
		msg.Content = content
		return msg
	}
	msg.Content = append([]agentkit.ContentPart{{Type: "text", Text: prefix}}, content...)
	return msg
}

func resolveUserTimezone(defaultTZ, userID string, platform agentkit.Platform) string {
	if platform != nil {
		if tzp, ok := platform.(agentkit.UserTimezoneProvider); ok {
			if tz := strings.TrimSpace(tzp.UserTimezone(userID)); tz != "" {
				if _, err := time.LoadLocation(tz); err == nil {
					return tz
				}
			}
		}
	}
	if tz := strings.TrimSpace(defaultTZ); tz != "" {
		return tz
	}
	return "UTC"
}

func formatInjectTimestamp(now time.Time, tzName string) string {
	tzName = strings.TrimSpace(tzName)
	if tzName == "" {
		tzName = "UTC"
	}
	loc, err := time.LoadLocation(tzName)
	if err != nil {
		loc = time.UTC
		tzName = "UTC"
	}
	local := now.In(loc)
	return fmt.Sprintf(`timestamp="%s" timezone="%s"`, local.Format(time.RFC3339), tzName)
}

func promptAttrValue(value string) string {
	return strings.NewReplacer(`"`, `'`, "\n", " ", "\r", "").Replace(value)
}

func senderNameFromMetadata(metadata map[string]any) string {
	return contextValueFromMetadata(metadata, "sender_name", "displayName", "userName", "name", "X-Chat-API-User-Name")
}

func senderEmailFromMetadata(metadata map[string]any) string {
	return contextValueFromMetadata(metadata, "sender_email", "email")
}

func contextValueFromMetadata(metadata map[string]any, keys ...string) string {
	for _, key := range keys {
		if v := metadataString(metadata, key); v != "" {
			return v
		}
	}
	return ""
}

func metadataString(metadata map[string]any, key string) string {
	if len(metadata) == 0 {
		return ""
	}
	raw, ok := metadata[key]
	if !ok {
		return ""
	}
	switch v := raw.(type) {
	case string:
		return strings.TrimSpace(v)
	default:
		return strings.TrimSpace(fmt.Sprint(v))
	}
}

func metadataPromptAttrs(metadata map[string]any, allowlist []string) []string {
	if len(metadata) == 0 || len(allowlist) == 0 {
		return nil
	}
	var attrs []string
	for _, key := range allowlist {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		if strings.HasSuffix(key, ".*") {
			prefix := strings.TrimSuffix(key, ".*")
			var matches []string
			for mk := range metadata {
				if mk == agentkit.MetadataSkipPromptMeta {
					continue
				}
				if strings.HasPrefix(mk, prefix) {
					matches = append(matches, mk)
				}
			}
			sort.Strings(matches)
			for _, mk := range matches {
				if v := metadataString(metadata, mk); v != "" {
					attrs = append(attrs, fmt.Sprintf(`%s="%s"`, mk, promptAttrValue(v)))
				}
			}
			continue
		}
		if v := metadataString(metadata, key); v != "" {
			attrs = append(attrs, fmt.Sprintf(`%s="%s"`, key, promptAttrValue(v)))
		}
	}
	return attrs
}
