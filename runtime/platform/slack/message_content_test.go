package slack

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/lengzhao/agentkit/runtime/platform/common"
	slackapi "github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
	"github.com/slack-go/slack/socketmode"
)

func TestStructuredContentReachesInbox(t *testing.T) {
	for _, mention := range []bool{false, true} {
		t.Run(map[bool]string{false: "message", true: "app mention"}[mention], func(t *testing.T) {
			raw := `{"type":"message","channel":"D123","channel_type":"im","user":"U123","blocks":[{"type":"section","text":{"type":"mrkdwn","text":"Card body"}}],"attachments":[{"text":"Forwarded body"}]}`
			var inner any
			if mention {
				ev := &slackevents.AppMentionEvent{}
				if err := json.Unmarshal([]byte(raw), ev); err != nil {
					t.Fatal(err)
				}
				inner = ev
			} else {
				ev := &slackevents.MessageEvent{}
				if err := json.Unmarshal([]byte(raw), ev); err != nil {
					t.Fatal(err)
				}
				inner = ev
			}
			p := &Platform{inbox: common.NewInbox(1)}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			p.handleEvent(ctx, socketmode.Event{Type: socketmode.EventTypeEventsAPI, Data: slackevents.EventsAPIEvent{Type: slackevents.CallbackEvent, InnerEvent: slackevents.EventsAPIInnerEvent{Data: inner}}})
			got, err := p.inbox.Receive(ctx)
			if err != nil {
				t.Fatal(err)
			}
			encoded, err := json.Marshal(got)
			if err != nil {
				t.Fatal(err)
			}
			for _, want := range []string{"Card body", "Forwarded body"} {
				if !strings.Contains(string(encoded), want) {
					t.Fatalf("missing %q in %s", want, encoded)
				}
			}
		})
	}
}

func TestSlackRichTextDefensiveTraversal(t *testing.T) {
	list := slackapi.NewRichTextList(slackapi.RTEListBullet, -1)
	list.Elements = []slackapi.RichTextElement{list, (*slackapi.RichTextSection)(nil)}
	msg := slackapi.NewBlockMessage(slackapi.NewRichTextBlock("", list, slackapi.NewRichTextSection((*slackapi.RichTextSectionTextElement)(nil))))
	if got := extractSlackBlockMessageText(msg); got != "" {
		t.Fatalf("got %q", got)
	}
}

func TestSlackMessageDoesNotDuplicateExactFallback(t *testing.T) {
	msg := slackapi.NewBlockMessage(slackapi.NewSectionBlock(slackapi.NewTextBlockObject(slackapi.MarkdownType, "Body", false, false), nil, nil))
	msg.Text = "Body"
	if got := extractSlackBlockMessageText(msg); got != "Body" {
		t.Fatalf("got %q", got)
	}
}
