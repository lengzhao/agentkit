package slack

import (
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
