package slack

import (
	"strings"
	"testing"

	slackapi "github.com/slack-go/slack"
)

func TestFormatSlackBlockActionOperation(t *testing.T) {
	action := &slackapi.BlockAction{
		ActionID: "approve_btn",
		Value:    `{"action":"go","session_key":"slack:C1:u:U1"}`,
	}
	got := formatSlackBlockActionOperation(action, "go", action.Value)
	if !strings.Contains(got, "action_id=approve_btn") || !strings.Contains(got, "action=go") {
		t.Fatalf("%q", got)
	}
}

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
