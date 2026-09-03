package slack

import (
	"testing"

	slackapi "github.com/slack-go/slack"
)

func TestHistoryMessageFromSlackUserText(t *testing.T) {
	t.Parallel()

	msg := historyMessageFromSlack(slackapi.Message{
		Msg: slackapi.Msg{
			Type:            "message",
			User:            "U123",
			Text:            "hello team",
			Timestamp:       "1710000001.000100",
			ThreadTimestamp: "1710000000.000000",
		},
	})
	if msg == nil {
		t.Fatal("expected message")
	}
	if msg.Role != "user" || msg.SenderID != "U123" || msg.Content != "hello team" {
		t.Fatalf("unexpected message: %+v", msg)
	}
	if msg.Timestamp != 1710000001 || msg.ThreadID != "1710000000.000000" {
		t.Fatalf("unexpected ids: %+v", msg)
	}
}

func TestHistoryMessageFromSlackSkipsDeleted(t *testing.T) {
	t.Parallel()

	if historyMessageFromSlack(slackapi.Message{
		Msg: slackapi.Msg{SubType: slackapi.MsgSubTypeMessageDeleted},
	}) != nil {
		t.Fatal("expected nil for deleted message")
	}
}

func TestHistoryMessageFromSlackBotMessage(t *testing.T) {
	t.Parallel()

	msg := historyMessageFromSlack(slackapi.Message{
		Msg: slackapi.Msg{
			SubType:   slackapi.MsgSubTypeBotMessage,
			BotID:     "B123",
			Username:  "ci-bot",
			Text:      "deploy finished",
			Timestamp: "1710000002.000100",
		},
	})
	if msg == nil || msg.Role != "assistant" || msg.SenderName != "ci-bot" {
		t.Fatalf("unexpected bot message: %+v", msg)
	}
}

func TestSlackOldestFromAfter(t *testing.T) {
	t.Parallel()

	if got := slackOldestFromAfter("1710000000"); got != "1710000000.000000" {
		t.Fatalf("got %q", got)
	}
	if got := slackOldestFromAfter("1710000000.123456"); got != "1710000000.123456" {
		t.Fatalf("got %q", got)
	}
}
