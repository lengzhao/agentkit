package chatapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/session"
)

func TestConversationIDFromSessionFile(t *testing.T) {
	channel := "default_channel"
	convID := "conv_xleOhmgad8IfiMcirKAYQw"
	name := safeSessionFileSegment(engineSessionKey(channel, convID)) + ".jsonl"
	got, ok := conversationIDFromSessionFile(channel, name, sessionFilePrefix(channel))
	if !ok {
		t.Fatalf("parse failed for %s", name)
	}
	if got != convID {
		t.Fatalf("got %q want %q", got, convID)
	}
}

func TestListConversationsFromPersistedSessions(t *testing.T) {
	root := t.TempDir()
	channel := "default_channel"
	convID := "conv_xleOhmgad8IfiMcirKAYQw"
	ws := staticWorkspace{root: root}
	store, err := session.NewStore(session.StoreConfig{Dir: "sessions"}, session.StoreDeps{Workspace: ws})
	if err != nil {
		t.Fatal(err)
	}
	if err := appendTestUserMessage(tenantCtx(channel, convID), store, channel, convID, "hi"); err != nil {
		t.Fatal(err)
	}

	p, err := New(Config{}, Deps{
		SessionStore: store,
		Workspace:    ws,
	})
	if err != nil {
		t.Fatal(err)
	}
	plat := p.(*Platform)

	list, err := plat.listConversations(context.Background(), channel, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("list len = %d, want 1", len(list))
	}
	if list[0].ID != convID {
		t.Fatalf("id = %q", list[0].ID)
	}
	if list[0].TurnCount != 1 {
		t.Fatalf("turn count = %d, want 1", list[0].TurnCount)
	}

	req := httptest.NewRequest(http.MethodGet, "/v1/conversations?user=demo", nil)
	req.Header.Set("X-Chat-API-Channel", channel)
	req.Header.Set("X-Chat-API-User", "demo")
	rec := httptest.NewRecorder()
	plat.handleConversations(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), convID) {
		t.Fatalf("body %s", rec.Body.String())
	}
}

func TestHistoryIgnoresChannelScopedSession(t *testing.T) {
	root := t.TempDir()
	channel := "default_channel"
	convID := "conv_xleOhmgad8IfiMcirKAYQw"
	ws := staticWorkspace{root: root}
	store, err := session.NewStore(session.StoreConfig{Dir: "sessions"}, session.StoreDeps{Workspace: ws})
	if err != nil {
		t.Fatal(err)
	}

	delivery := agentkit.SessionID(engineSessionKey(channel, convID))
	effective := session.ApplyScope(delivery, session.ScopeChannel, "demo")
	ctx := session.ApplyEnvelopeToContext(context.Background(), agentkit.TurnEnvelope{Conversation: string(effective)})
	sess, err := store.Get(ctx, effective)
	if err != nil {
		t.Fatal(err)
	}
	if err := session.AppendMessage(ctx, sess, "coder", agentkit.EventUserMessage, agentkit.ModelMessage{
		Role:    "user",
		Content: []agentkit.ContentPart{{Type: "text", Text: "channel-only"}},
	}); err != nil {
		t.Fatal(err)
	}

	p, err := New(Config{}, Deps{SessionStore: store, Workspace: ws})
	if err != nil {
		t.Fatal(err)
	}
	plat := p.(*Platform)
	idxPath, err := plat.indexPathForChannel(tenantCtx(channel, convID), channel)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(idxPath), 0o755); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(conversationIndexFile{Conversations: []persistedConversation{{
		ID:        convID,
		CreatedBy: "demo",
		CreatedAt: 1,
		UpdatedAt: 2,
		TurnCount: 1,
	}}})
	if err := os.WriteFile(idxPath, raw, 0o644); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/v1/conversations/"+convID+"/messages?limit=10", nil)
	req.Header.Set("X-Chat-API-Channel", channel)
	req.Header.Set("X-Chat-API-User", "demo")
	rec := httptest.NewRecorder()
	plat.handleConversationMessages(rec, req, convID)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "channel-only") {
		t.Fatalf("channel-scoped agent history must not leak into conversation API: %s", rec.Body.String())
	}
}

func TestConversationMessagesFromAgentSession(t *testing.T) {
	root := t.TempDir()
	channel := "default_channel"
	convID := "conv_xleOhmgad8IfiMcirKAYQw"
	ws := staticWorkspace{root: root}
	store, err := session.NewStore(session.StoreConfig{Dir: "sessions"}, session.StoreDeps{Workspace: ws})
	if err != nil {
		t.Fatal(err)
	}
	ctx := tenantCtx(channel, convID)
	if err := appendTestUserMessage(ctx, store, channel, convID, "hello"); err != nil {
		t.Fatal(err)
	}
	sess, err := store.Get(ctx, agentkit.SessionID(engineSessionKey(channel, convID)))
	if err != nil {
		t.Fatal(err)
	}
	if err := session.AppendMessage(ctx, sess, "assistant", agentkit.EventAssistantMessage, agentkit.ModelMessage{
		Role:    "assistant",
		Content: []agentkit.ContentPart{{Type: "text", Text: "world"}},
	}); err != nil {
		t.Fatal(err)
	}

	p, err := New(Config{}, Deps{SessionStore: store, Workspace: ws})
	if err != nil {
		t.Fatal(err)
	}
	plat := p.(*Platform)

	req := httptest.NewRequest(http.MethodGet, "/v1/conversations/"+convID+"/messages?limit=10", nil)
	req.Header.Set("X-Chat-API-Channel", channel)
	req.Header.Set("X-Chat-API-User", "demo")
	rec := httptest.NewRecorder()
	plat.handleConversationMessages(rec, req, convID)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "hello") || !strings.Contains(body, "world") {
		t.Fatalf("body %s", body)
	}
}

func TestResolveConversationAfterRestart(t *testing.T) {
	root := t.TempDir()
	channel := "default_channel"
	convID := "conv_xleOhmgad8IfiMcirKAYQw"
	ws := staticWorkspace{root: root}
	store, err := session.NewStore(session.StoreConfig{Dir: "sessions"}, session.StoreDeps{Workspace: ws})
	if err != nil {
		t.Fatal(err)
	}
	if err := appendTestUserMessage(tenantCtx(channel, convID), store, channel, convID, "hello"); err != nil {
		t.Fatal(err)
	}

	p, err := New(Config{}, Deps{SessionStore: store, Workspace: ws})
	if err != nil {
		t.Fatal(err)
	}
	plat := p.(*Platform)

	c, err := plat.resolveConversation(context.Background(), channel, convID, "demo")
	if err != nil {
		t.Fatal(err)
	}
	if c == nil {
		t.Fatal("expected conversation from session file")
	}
	if c.TurnCount != 1 {
		t.Fatalf("turn count = %d", c.TurnCount)
	}
}

func appendTestUserMessage(ctx context.Context, store agentkit.SessionStore, channel, convID, text string) error {
	sess, err := store.Get(ctx, agentkit.SessionID(engineSessionKey(channel, convID)))
	if err != nil {
		return err
	}
	return session.AppendMessage(ctx, sess, "coder", agentkit.EventUserMessage, agentkit.ModelMessage{
		Role:    "user",
		Content: []agentkit.ContentPart{{Type: "text", Text: text}},
	})
}

func tenantCtx(channel, convID string) context.Context {
	return session.ApplyEnvelopeToContext(context.Background(), agentkit.TurnEnvelope{Conversation: string(engineSessionKey(channel, convID))})
}

func TestPersistedSessionSurvivesStoreReopen(t *testing.T) {
	root := t.TempDir()
	channel := "default_channel"
	convID := "conv_xleOhmgad8IfiMcirKAYQw"
	ws := staticWorkspace{root: root}

	store1, err := session.NewStore(session.StoreConfig{Dir: "sessions"}, session.StoreDeps{Workspace: ws})
	if err != nil {
		t.Fatal(err)
	}
	if err := appendTestUserMessage(tenantCtx(channel, convID), store1, channel, convID, "persist"); err != nil {
		t.Fatal(err)
	}

	store2, err := session.NewStore(session.StoreConfig{Dir: "sessions"}, session.StoreDeps{Workspace: ws})
	if err != nil {
		t.Fatal(err)
	}
	p, err := New(Config{}, Deps{SessionStore: store2, Workspace: ws})
	if err != nil {
		t.Fatal(err)
	}
	plat := p.(*Platform)

	list, err := plat.listConversations(context.Background(), channel, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].ID != convID {
		t.Fatalf("list = %+v", list)
	}

	sessionsAbs, err := ws.Resolve(tenantCtx(channel, convID), "sessions")
	if err != nil {
		t.Fatal(err)
	}
	name := safeSessionFileSegment(engineSessionKey(channel, convID)) + ".jsonl"
	if _, err := os.Stat(filepath.Join(sessionsAbs, name)); err != nil {
		t.Fatalf("session file missing: %v", err)
	}
}
