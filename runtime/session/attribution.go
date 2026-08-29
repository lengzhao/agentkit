package session

import (
	"bytes"
	"context"
	"log/slog"
	"sync"
	"text/template"

	"github.com/lengzhao/agentkit"
)

// LegacyUserMessageTemplate is the default user-message wrapper when
// sessionStore.config.userMessageTemplate is unset.
const LegacyUserMessageTemplate = "<user id=\"{{.UserID}}\">\n{{.Text}}\n</user>"

// UserMessageTemplateData is passed to userMessageTemplate on replay.
// Use Go text/template syntax, e.g. [user id={{.UserID}}]\n{{.Text}} or
// {{index .Metadata "displayName"}}.
type UserMessageTemplateData struct {
	UserID   string
	Text     string
	Metadata map[string]any
}

var userMessageTemplateCache sync.Map // string -> *template.Template

func resolveUserMessageTemplate(ctx context.Context, configured string) string {
	if override, ok := ctx.Value(agentkit.KeyUserMessageTemplate).(string); ok && override != "" {
		return override
	}
	if configured != "" {
		return configured
	}
	return LegacyUserMessageTemplate
}

func parseUserMessageTemplate(tmpl string) (*template.Template, error) {
	if cached, ok := userMessageTemplateCache.Load(tmpl); ok {
		return cached.(*template.Template), nil
	}
	parsed, err := template.New("userMessage").Parse(tmpl)
	if err != nil {
		return nil, err
	}
	userMessageTemplateCache.Store(tmpl, parsed)
	return parsed, nil
}

func renderUserMessageTemplate(tmpl string, data UserMessageTemplateData) (string, error) {
	parsed, err := parseUserMessageTemplate(tmpl)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := parsed.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// applyUserMessageTemplate renders a template for user messages that carry a
// UserID. Empty UserID leaves the stored content unchanged.
func applyUserMessageTemplate(msg agentkit.ModelMessage, userID string, metadata map[string]any, tmpl string) agentkit.ModelMessage {
	if tmpl == "" || userID == "" {
		return msg
	}
	content := make([]agentkit.ContentPart, len(msg.Content))
	copy(content, msg.Content)
	msg.Content = content
	for i, part := range content {
		if part.Type != "text" {
			continue
		}
		rendered, err := renderUserMessageTemplate(tmpl, UserMessageTemplateData{
			UserID:   userID,
			Text:     part.Text,
			Metadata: metadata,
		})
		if err != nil {
			slog.Warn("session: user message template failed, using stored text",
				"user_id", userID,
				"err", err,
			)
			return msg
		}
		content[i].Text = rendered
		return msg
	}
	rendered, err := renderUserMessageTemplate(tmpl, UserMessageTemplateData{
		UserID:   userID,
		Metadata: metadata,
	})
	if err != nil {
		slog.Warn("session: user message template failed, using stored content",
			"user_id", userID,
			"err", err,
		)
		return msg
	}
	msg.Content = append([]agentkit.ContentPart{{Type: "text", Text: rendered}}, content...)
	return msg
}
