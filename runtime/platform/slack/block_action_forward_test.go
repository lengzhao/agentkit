package slack

import (
	"encoding/json"
	"strings"
	"testing"

	slackapi "github.com/slack-go/slack"
)

func TestExtractSlackBlockMessageText_fromBlocks(t *testing.T) {
	msg := slackapi.NewBlockMessage(
		slackapi.NewSectionBlock(
			slackapi.NewTextBlockObject(slackapi.MarkdownType, "*标题*", false, false),
			nil, nil,
		),
	)
	got := extractSlackBlockMessageText(msg)
	if !strings.Contains(got, "标题") {
		t.Fatalf("%q", got)
	}
}

func TestExtractSlackMessageStructuredContent(t *testing.T) {
	for _, tc := range []struct {
		name, raw string
		want      []string
	}{
		{"summary and actions", `{"text":"Summary","blocks":[{"type":"section","text":{"type":"mrkdwn","text":"Details"},"accessory":{"type":"button","text":{"type":"plain_text","text":"Open"},"url":"https://example.com"}},{"type":"image","image_url":"https://example.com/i.png","alt_text":"Screenshot"}]}`, []string{"Summary", "Details", "[Button: Open](https://example.com)", "Screenshot"}},
		{"forwarded attachment", `{"text":"Look at this","attachments":[{"title":"Original","title_link":"https://example.com/original","text":"Forwarded body","fields":[{"title":"Status","value":"Ready"}],"blocks":[{"type":"section","text":{"type":"mrkdwn","text":"Nested block"}}]}]}`, []string{"Look at this", "[Original](https://example.com/original)", "Forwarded body", "Status: Ready", "Nested block"}},
		{"rich text", `{"blocks":[{"type":"rich_text","elements":[{"type":"rich_text_section","elements":[{"type":"text","text":"Hello "},{"type":"text","text":"world","style":{"bold":true}},{"type":"link","text":" docs","url":"https://example.com/docs"}]},{"type":"rich_text_list","style":"bullet","indent":-1,"elements":[{"type":"rich_text_section","elements":[{"type":"text","text":"Item"}]}]},{"type":"rich_text_quote","elements":[{"type":"text","text":"Quoted"}]},{"type":"rich_text_preformatted","elements":[{"type":"text","text":"a\nb"}]}]}]}`, []string{"Hello **world**", "https://example.com/docs", "- Item", "> Quoted", "```\na\nb\n```"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var msg slackapi.Message
			if err := json.Unmarshal([]byte(tc.raw), &msg); err != nil {
				t.Fatal(err)
			}
			got := extractSlackBlockMessageText(msg)
			for _, want := range tc.want {
				if !strings.Contains(got, want) {
					t.Errorf("missing %q in %q", want, got)
				}
			}
			history := historyMessageFromSlack(msg)
			if history == nil || history.Content != got {
				t.Errorf("history lost content: %+v; want %q", history, got)
			}
		})
	}
}

func TestExtractSlackMessageEmptyAndNilBlocks(t *testing.T) {
	msg := slackapi.NewBlockMessage((*slackapi.SectionBlock)(nil), (*slackapi.RichTextBlock)(nil))
	if got := extractSlackBlockMessageText(msg); got != "" {
		t.Fatalf("got %q", got)
	}
}

func TestExtractSlackMessageBoundsListIndent(t *testing.T) {
	msg := slackapi.NewBlockMessage(slackapi.NewRichTextBlock("", slackapi.NewRichTextList(slackapi.RTEListBullet, 1000000, slackapi.NewRichTextSection(slackapi.NewRichTextSectionTextElement("item", nil)))))
	got := extractSlackBlockMessageText(msg)
	if want := strings.Repeat("  ", 16) + "- item"; got != want {
		t.Fatalf("got length %d, want %q", len(got), want)
	}
}
