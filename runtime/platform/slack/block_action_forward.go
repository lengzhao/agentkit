package slack

import (
	"strings"

	slackapi "github.com/slack-go/slack"
)

func extractSlackBlockMessageText(msg slackapi.Message) string {
	text := strings.TrimSpace(msg.Text)
	if text != "" {
		return text
	}
	if msg.Blocks.BlockSet == nil {
		return historyFallbackText(msg)
	}
	var parts []string
	for _, block := range msg.Blocks.BlockSet {
		switch b := block.(type) {
		case *slackapi.SectionBlock:
			if b.Text != nil && strings.TrimSpace(b.Text.Text) != "" {
				parts = append(parts, b.Text.Text)
			}
			for _, f := range b.Fields {
				if f != nil && strings.TrimSpace(f.Text) != "" {
					parts = append(parts, f.Text)
				}
			}
		case *slackapi.HeaderBlock:
			if b.Text != nil && strings.TrimSpace(b.Text.Text) != "" {
				parts = append(parts, b.Text.Text)
			}
		case *slackapi.ContextBlock:
			for _, el := range b.ContextElements.Elements {
				if te, ok := el.(*slackapi.TextBlockObject); ok && strings.TrimSpace(te.Text) != "" {
					parts = append(parts, te.Text)
				}
			}
		case *slackapi.RichTextBlock:
			parts = append(parts, extractRichTextBlockPlain(b))
		}
	}
	if len(parts) == 0 {
		return historyFallbackText(msg)
	}
	return strings.Join(parts, "\n")
}

func extractRichTextBlockPlain(b *slackapi.RichTextBlock) string {
	if b == nil {
		return ""
	}
	var parts []string
	for _, el := range b.Elements {
		switch section := el.(type) {
		case *slackapi.RichTextSection:
			for _, sub := range section.Elements {
				if te, ok := sub.(*slackapi.RichTextSectionTextElement); ok {
					if t := strings.TrimSpace(te.Text); t != "" {
						parts = append(parts, t)
					}
				}
			}
		}
	}
	return strings.Join(parts, " ")
}
