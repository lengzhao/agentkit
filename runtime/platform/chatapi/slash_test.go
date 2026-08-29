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
	"github.com/lengzhao/agentkit/cap/workspace"
	"github.com/lengzhao/agentkit/runtime/command"
	"github.com/lengzhao/agentkit/runtime/platform/common"
	"github.com/lengzhao/agentkit/runtime/session"
)

func TestProcessChatSlashNewKeepsConversationAndMapsActiveSession(t *testing.T) {
	store, err := session.NewStore(session.StoreConfig{Dir: "."}, session.StoreDeps{Workspace: workspace.Static(t.TempDir())})
	if err != nil {
		t.Fatal(err)
	}
	commands, err := command.NewFromProviders(command.Config{}, []agentkit.CommandProvider{store.(agentkit.CommandProvider)})
	if err != nil {
		t.Fatal(err)
	}
	p, err := New(Config{}, Deps{SessionStore: store, Commands: commands})
	if err != nil {
		t.Fatal(err)
	}
	plat := p.(*Platform)

	oldConv, err := plat.conversations.create("default_channel", "demo")
	if err != nil {
		t.Fatal(err)
	}

	stable := agentkit.SessionID(engineSessionKey("default_channel", oldConv.ID))
	result, err := plat.processChatSlash(context.Background(), "default_channel", oldConv, stable, "/new")
	if err != nil {
		t.Fatal(err)
	}
	if result.outcome.Kind != common.SlashHandled {
		t.Fatalf("kind = %v", result.outcome.Kind)
	}
	if !strings.HasPrefix(result.outcome.Reply, string(stable)+":new:") {
		t.Fatalf("reply = %q", result.outcome.Reply)
	}
	active, err := store.(agentkit.ActiveSessionStore).ActiveSession(context.Background(), stable)
	if err != nil {
		t.Fatal(err)
	}
	if active != agentkit.SessionID(result.outcome.Reply) {
		t.Fatalf("active session = %q, want %q", active, result.outcome.Reply)
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
	store, err := session.NewStore(session.StoreConfig{Dir: "."}, session.StoreDeps{Workspace: workspace.Static(t.TempDir())})
	if err != nil {
		t.Fatal(err)
	}
	commands, err := command.NewFromProviders(command.Config{}, []agentkit.CommandProvider{store.(agentkit.CommandProvider)})
	if err != nil {
		t.Fatal(err)
	}
	p, err := New(Config{Path: "/v1/"}, Deps{SessionStore: store, Commands: commands})
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
	if !strings.Contains(out, "chat-api:default_channel:t:") || !strings.Contains(out, ":new:") {
		t.Fatalf("missing logical session reply: %s", out)
	}
	if strings.Contains(out, "cli:") {
		t.Fatalf("should not return CLI session id: %s", out)
	}
}
