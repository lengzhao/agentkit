package slack

import (
	"fmt"
	"reflect"
	"strings"

	slackapi "github.com/slack-go/slack"
)

// extractSlackBlockMessageText keeps structured content even when text is only
// a notification summary. Identical fallback text is emitted only once.
func extractSlackBlockMessageText(msg slackapi.Message) string {
	var parts []string
	add := func(s string) {
		if strings.TrimSpace(s) != "" {
			parts = append(parts, s)
		}
	}
	structured := slackBlocksText(msg.Blocks)
	text := strings.TrimSpace(msg.Text)
	if text != strings.TrimSpace(structured) {
		add(text)
	}
	add(structured)
	for _, a := range msg.Attachments {
		var attachment []string
		appendPart := func(s string) {
			if strings.TrimSpace(s) != "" {
				attachment = append(attachment, s)
			}
		}
		appendPart(a.Pretext)
		appendPart(slackTextLink(a.Title, a.TitleLink))
		appendPart(a.Text)
		for _, f := range a.Fields {
			appendPart(strings.Trim(strings.TrimSpace(f.Title)+": "+f.Value, ": "))
		}
		appendPart(slackBlocksText(a.Blocks))
		for _, action := range a.Actions {
			appendPart(slackButtonText(action.Text, action.URL))
		}
		appendPart(a.Footer)
		if len(attachment) == 0 {
			appendPart(a.Fallback)
		}
		add(strings.Join(attachment, "\n"))
	}
	if len(parts) == 0 {
		return historyFallbackText(msg)
	}
	return strings.Join(parts, "\n")
}

// SDK interfaces can hold typed nil pointers as well as nil interfaces.
func nilSlackElement(v any) bool {
	if v == nil {
		return true
	}
	r := reflect.ValueOf(v)
	return r.Kind() == reflect.Pointer && r.IsNil()
}

func slackBlocksText(blocks slackapi.Blocks) string {
	var parts []string
	add := func(s string) {
		if strings.TrimSpace(s) != "" {
			parts = append(parts, s)
		}
	}
	for _, block := range blocks.BlockSet {
		if nilSlackElement(block) {
			continue
		}
		switch b := block.(type) {
		case *slackapi.SectionBlock:
			if b.Text != nil {
				add(b.Text.Text)
			}
			for _, f := range b.Fields {
				if f != nil {
					add(f.Text)
				}
			}
			if b.Accessory != nil {
				add(slackBlockElementText(b.Accessory.ButtonElement))
				add(slackBlockElementText(b.Accessory.ImageElement))
			}
		case *slackapi.HeaderBlock:
			if b.Text != nil {
				add(b.Text.Text)
			}
		case *slackapi.ContextBlock:
			for _, el := range b.ContextElements.Elements {
				if nilSlackElement(el) {
					continue
				}
				switch e := el.(type) {
				case *slackapi.TextBlockObject:
					add(e.Text)
				case *slackapi.ImageBlockElement:
					add(slackBlockElementText(e))
				}
			}
		case *slackapi.ActionBlock:
			if b.Elements != nil {
				for _, el := range b.Elements.ElementSet {
					add(slackBlockElementText(el))
				}
			}
		case *slackapi.ImageBlock:
			if b.Title != nil {
				add(b.Title.Text)
			}
			add("[Image: " + b.AltText + "]")
		case *slackapi.RichTextBlock:
			add(extractRichTextBlockPlain(b))
		}
	}
	return strings.Join(parts, "\n")
}

func slackTextLink(text, url string) string {
	if url == "" {
		return text
	}
	if text == "" {
		return url
	}
	return "[" + text + "](" + url + ")"
}

func slackButtonText(text, url string) string {
	if text == "" {
		text = url
	}
	if text == "" {
		return ""
	}
	return slackTextLink("Button: "+text, url)
}

func slackBlockElementText(el slackapi.BlockElement) string {
	if nilSlackElement(el) {
		return ""
	}
	switch e := el.(type) {
	case *slackapi.ButtonBlockElement:
		text := ""
		if e.Text != nil {
			text = e.Text.Text
		}
		return slackButtonText(text, e.URL)
	case *slackapi.ImageBlockElement:
		return "[Image: " + e.AltText + "]"
	}
	return ""
}

func extractRichTextBlockPlain(b *slackapi.RichTextBlock) string {
	if b == nil {
		return ""
	}
	var parts []string
	for _, el := range b.Elements {
		if s := slackRichElementText(el, 0); strings.TrimSpace(s) != "" {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, "\n")
}

func slackRichElementText(el slackapi.RichTextElement, depth int) string {
	// Lists are recursive SDK values; bound both recursion and untrusted indent.
	if depth >= 32 || nilSlackElement(el) {
		return ""
	}
	switch e := el.(type) {
	case *slackapi.RichTextSection:
		return slackRichInlineText(e.Elements)
	case *slackapi.RichTextList:
		var lines []string
		for i, child := range e.Elements {
			text := slackRichElementText(child, depth+1)
			if strings.TrimSpace(text) == "" {
				continue
			}
			marker := "- "
			if e.Style == slackapi.RTEListOrdered {
				marker = fmt.Sprintf("%d. ", i+1)
			}
			lines = append(lines, strings.Repeat("  ", min(max(e.Indent, 0), 16))+marker+text)
		}
		return strings.Join(lines, "\n")
	case *slackapi.RichTextQuote:
		return "> " + strings.ReplaceAll(slackRichInlineText(e.Elements), "\n", "\n> ")
	case *slackapi.RichTextPreformatted:
		return "```" + e.Language + "\n" + slackRichInlineText(e.Elements) + "\n```"
	}
	return ""
}

func slackRichInlineText(elements []slackapi.RichTextSectionElement) string {
	var out strings.Builder
	for _, el := range elements {
		if nilSlackElement(el) {
			continue
		}
		switch e := el.(type) {
		case *slackapi.RichTextSectionTextElement:
			text := e.Text
			core := strings.TrimSpace(text)
			if core != "" && e.Style != nil {
				styled := core
				if e.Style.Code {
					styled = "`" + styled + "`"
				}
				if e.Style.Bold {
					styled = "**" + styled + "**"
				}
				if e.Style.Italic {
					styled = "*" + styled + "*"
				}
				if e.Style.Strike {
					styled = "~~" + styled + "~~"
				}
				i := strings.Index(text, core)
				text = text[:i] + styled + text[i+len(core):]
			}
			out.WriteString(text)
		case *slackapi.RichTextSectionLinkElement:
			out.WriteString(slackTextLink(e.Text, e.URL))
		case *slackapi.RichTextSectionUserElement:
			out.WriteString("<@" + e.UserID + ">")
		case *slackapi.RichTextSectionChannelElement:
			out.WriteString("<#" + e.ChannelID + ">")
		case *slackapi.RichTextSectionEmojiElement:
			out.WriteString(":" + e.Name + ":")
		case *slackapi.RichTextSectionBroadcastElement:
			out.WriteString("<!" + e.Range + ">")
		}
	}
	return out.String()
}
