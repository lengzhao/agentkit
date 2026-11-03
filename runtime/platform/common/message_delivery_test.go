package common

import (
	"errors"
	"strings"
	"testing"

	cap "github.com/lengzhao/agentkit/cap/delivery"
	"github.com/lengzhao/agentkit/runtime/rctx"
)

func TestMessageContentRejectsAllBeforePartialSend(t *testing.T) {
	for _, parts := range [][]cap.Part{
		{{Type: "text", Text: strings.Repeat("x", 4000)}, {Type: "image"}},
		{{Type: "mention", UserID: "<@everyone>"}},
		{{Type: "text", Text: "\xff"}},
		{},
	} {
		if _, err := SplitMessageContent(parts); err == nil {
			t.Fatalf("accepted %+v", parts)
		}
	}
	chunks, err := SplitMessageContent([]cap.Part{{Type: "text", Text: strings.Repeat("中", 3001)}, {Type: "mention", UserID: "user"}})
	if err != nil || len(chunks) != 2 || len([]rune(chunks[0][0].Text)) != 3000 {
		t.Fatalf("%+v %v", chunks, err)
	}
}
func TestMessageKeysScopeAndPartialOutcome(t *testing.T) {
	req := cap.PublishRequest{Route: rctx.SessionRoute("test", "test:channel").WithInstance("a"), Caller: "consumer", OperationID: "stable"}
	key := MessageOperationKey(req, "publish", 0)
	if key == "" || key != MessageOperationKey(req, "publish", 0) || key == MessageOperationKey(req, "publish", 1) || key == MessageOperationKey(req, "create_topic", 0) {
		t.Fatal("unstable or colliding key")
	}
	req.Route.Instance = "b"
	if key == MessageOperationKey(req, "publish", 0) {
		t.Fatal("instance key collision")
	}
	receipt, err := FailMessagePart(cap.Receipt{Messages: []cap.MessageRef{{ID: "sent"}}}, 1, &cap.Error{Code: cap.CodeRateLimited, Outcome: cap.OutcomeNotSent})
	var de *cap.Error
	if receipt.Outcome != cap.OutcomePartial || !errors.As(err, &de) || de.Outcome != cap.OutcomeNotSent {
		t.Fatalf("%+v %v", receipt, err)
	}
}
