package chatapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/platform/common"
)

func TestProcessChatSlashNewCreatesConversation(t *testing.T) {
	p, err := New(Config{}, Deps{})
	if err != nil {
		t.Fatal(err)
	}
	plat := p.(*Platform)

	oldConv, err := plat.conversations.create("default_channel", "demo")
	if err != nil {
		t.Fatal(err)
	}

	result, err := plat.processChatSlash(context.Background(), "default_channel", oldConv, agentkit.SessionID(engineSessionKey("default_channel", oldConv.ID)), "/new")
	if err != nil {
		t.Fatal(err)
	}
	if result.outcome.Kind != common.SlashHandled {
		t.Fatalf("kind = %v", result.outcome.Kind)
	}
	if !strings.HasPrefix(result.outcome.Reply, "已开始新会话：conv_") {
		t.Fatalf("reply = %q", result.outcome.Reply)
	}
	if result.switchConversationID == "" || result.switchConversationID == oldConv.ID {
		t.Fatalf("switchConversationID = %q", result.switchConversationID)
	}
	if got := plat.conversations.get(result.switchConversationID); got == nil {
		t.Fatal("new conversation not registered")
	}
}

func TestProcessChatSlashSessionUsesConversation(t *testing.T) {
	p, err := New(Config{}, Deps{})
	if err != nil {
		t.Fatal(err)
	}
	plat := p.(*Platform)

	conv, err := plat.conversations.create("default_channel", "demo")
	if err != nil {
		t.Fatal(err)
	}
	plat.conversations.bumpTurn(conv.ID)

	sessionID := agentkit.SessionID(engineSessionKey("default_channel", conv.ID))
	result, err := plat.processChatSlash(context.Background(), "default_channel", conv, sessionID, "/session")
	if err != nil {
		t.Fatal(err)
	}
	if result.outcome.Kind != common.SlashHandled {
		t.Fatalf("kind = %v", result.outcome.Kind)
	}
	if strings.Contains(result.outcome.Reply, "cli:") {
		t.Fatalf("should not return CLI session id: %q", result.outcome.Reply)
	}
	if !strings.Contains(result.outcome.Reply, conv.ID) {
		t.Fatalf("missing conversation id: %q", result.outcome.Reply)
	}
	if !strings.Contains(result.outcome.Reply, string(sessionID)) {
		t.Fatalf("missing engine session id: %q", result.outcome.Reply)
	}
}

func TestServeNewConversationSlash(t *testing.T) {
	p, err := New(Config{Path: "/v1/"}, Deps{})
	if err != nil {
		t.Fatal(err)
	}
	plat := p.(*Platform)

	body, _ := json.Marshal(chatRequest{Query: "/new"})
	req := httptest.NewRequest(http.MethodPost, "/v1/chat-messages", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("X-Chat-API-Channel", "default_channel")
	req.Header.Set("X-Chat-API-User", "demo")
	rec := httptest.NewRecorder()
	plat.handleChatMessages(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	out := rec.Body.String()
	if !strings.Contains(out, "已开始新会话：conv_") {
		t.Fatalf("missing new conversation reply: %s", out)
	}
	if strings.Contains(out, "cli:") {
		t.Fatalf("should not return CLI session id: %s", out)
	}
}
