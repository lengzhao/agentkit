package feishu

import (
	"context"
	"sync"
	"testing"
	"time"

	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"

	"github.com/lengzhao/agentkit"
)

type recordingCommands struct {
	mu         sync.Mutex
	dispatched []string
}

func (c *recordingCommands) Dispatch(_ context.Context, name string, rawArgs string) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.dispatched = append(c.dispatched, name+" "+rawArgs)
	return "stopping current turn", nil
}

func (c *recordingCommands) List(_ context.Context) []agentkit.Command { return nil }

func (c *recordingCommands) calls() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.dispatched...)
}

func recallEvent(messageID, chatID string) *larkim.P2MessageRecalledV1 {
	return &larkim.P2MessageRecalledV1{
		Event: &larkim.P2MessageRecalledV1Data{
			MessageId: strPtr(messageID),
			ChatId:    strPtr(chatID),
		},
	}
}

func newRecallTestPlatform(commands agentkit.Commands, stopOnRecall bool) *Platform {
	return &Platform{
		platformTag:  "feishu",
		commands:     commands,
		sessionScope: agentkit.SessionScopeChannel,
		stopOnRecall: stopOnRecall,
	}
}

func storeActiveTurnBatch(p *Platform, sessionID agentkit.SessionID, rcs ...replyContext) {
	batch := p.turnReactionBatchFor(sessionID)
	batch.mu.Lock()
	defer batch.mu.Unlock()
	batch.active = true
	batch.rcs = append([]replyContext(nil), rcs...)
}

func TestRecallTriggerMessageStopsTurn(t *testing.T) {
	commands := &recordingCommands{}
	p := newRecallTestPlatform(commands, true)
	sessionID := agentkit.SessionID("feishu:oc_chat")
	storeActiveTurnBatch(p, sessionID,
		replyContext{messageID: "om_trigger", chatID: "oc_chat"},
		replyContext{messageID: "om_steered", chatID: "oc_chat"},
	)

	p.cancelTurnsForRecalledMessage("om_trigger")
	calls := commands.calls()
	if len(calls) != 1 || calls[0] != "stop " {
		t.Fatalf("expected one /stop dispatch, got %v", calls)
	}
}

func TestRecallSteeredMessageStopsTurn(t *testing.T) {
	commands := &recordingCommands{}
	p := newRecallTestPlatform(commands, true)
	sessionID := agentkit.SessionID("feishu:oc_chat")
	storeActiveTurnBatch(p, sessionID,
		replyContext{messageID: "om_trigger", chatID: "oc_chat"},
		replyContext{messageID: "om_steered", chatID: "oc_chat"},
	)

	p.cancelTurnsForRecalledMessage("om_steered")
	if calls := commands.calls(); len(calls) != 1 {
		t.Fatalf("expected one /stop dispatch, got %v", calls)
	}
}

func TestRecallUnrelatedMessageDoesNotStopTurn(t *testing.T) {
	commands := &recordingCommands{}
	p := newRecallTestPlatform(commands, true)
	sessionID := agentkit.SessionID("feishu:oc_chat")
	storeActiveTurnBatch(p, sessionID,
		replyContext{messageID: "om_trigger", chatID: "oc_chat"},
	)

	p.cancelTurnsForRecalledMessage("om_old")
	if calls := commands.calls(); len(calls) != 0 {
		t.Fatalf("expected no dispatch for unrelated recall, got %v", calls)
	}
}

func TestRecallFinishedTurnDoesNotStop(t *testing.T) {
	commands := &recordingCommands{}
	p := newRecallTestPlatform(commands, true)
	sessionID := agentkit.SessionID("feishu:oc_chat")
	storeActiveTurnBatch(p, sessionID,
		replyContext{messageID: "om_trigger", chatID: "oc_chat"},
	)
	p.finishTurnReactions(sessionID)

	p.cancelTurnsForRecalledMessage("om_trigger")
	if calls := commands.calls(); len(calls) != 0 {
		t.Fatalf("expected no dispatch for finished turn, got %v", calls)
	}
}

// onMessageRecalled 的取消派发是异步的（不阻塞 SDK 事件循环），轮询等待。
func TestOnMessageRecalledDispatchesStopAsync(t *testing.T) {
	commands := &recordingCommands{}
	p := newRecallTestPlatform(commands, true)
	sessionID := agentkit.SessionID("feishu:oc_chat")
	storeActiveTurnBatch(p, sessionID,
		replyContext{messageID: "om_trigger", chatID: "oc_chat"},
	)

	if err := p.onMessageRecalled(context.Background(), recallEvent("om_trigger", "oc_chat")); err != nil {
		t.Fatalf("onMessageRecalled: %v", err)
	}
	if !p.isMessageRecalled("om_trigger") {
		t.Fatal("expected message marked as recalled")
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if calls := commands.calls(); len(calls) == 1 {
			if calls[0] != "stop " {
				t.Fatalf("expected /stop dispatch, got %v", calls)
			}
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("expected async /stop dispatch after recall")
}

func TestRecallStopDisabled(t *testing.T) {
	commands := &recordingCommands{}
	p := newRecallTestPlatform(commands, false)
	sessionID := agentkit.SessionID("feishu:oc_chat")
	storeActiveTurnBatch(p, sessionID,
		replyContext{messageID: "om_trigger", chatID: "oc_chat"},
	)

	if err := p.onMessageRecalled(context.Background(), recallEvent("om_trigger", "oc_chat")); err != nil {
		t.Fatalf("onMessageRecalled: %v", err)
	}
	if calls := commands.calls(); len(calls) != 0 {
		t.Fatalf("expected no dispatch when stopOnRecall=false, got %v", calls)
	}
	// 标记逻辑不受开关影响。
	if !p.isMessageRecalled("om_trigger") {
		t.Fatal("expected message marked as recalled even when stopOnRecall=false")
	}
}

func TestRecallFromUnauthorizedChatIgnored(t *testing.T) {
	commands := &recordingCommands{}
	p := newRecallTestPlatform(commands, true)
	p.allowChat = "oc_allowed"
	sessionID := agentkit.SessionID("feishu:oc_other")
	storeActiveTurnBatch(p, sessionID,
		replyContext{messageID: "om_trigger", chatID: "oc_other"},
	)

	if err := p.onMessageRecalled(context.Background(), recallEvent("om_trigger", "oc_other")); err != nil {
		t.Fatalf("onMessageRecalled: %v", err)
	}
	if calls := commands.calls(); len(calls) != 0 {
		t.Fatalf("expected no dispatch for unauthorized chat, got %v", calls)
	}
}
