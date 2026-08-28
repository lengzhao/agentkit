package chatapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/session"
)

func TestEngineSessionKeyEncodesColon(t *testing.T) {
	got := engineSessionKey("team:alpha", "conv_abc1234567890123456789")
	want := "chat-api:team%3Aalpha:t:conv_abc1234567890123456789"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestChatOutboundSSE(t *testing.T) {
	p, err := New(Config{ListenAddr: ":0"}, Deps{})
	if err != nil {
		t.Fatal(err)
	}
	plat := p.(*Platform)

	runID := newRunID()
	conv, err := plat.conversations.create("default_channel", "u1")
	if err != nil {
		t.Fatal(err)
	}
	sessionID := agentkit.SessionID(engineSessionKey("default_channel", conv.ID))
	rec := httptest.NewRecorder()
	sse, err := newSSEWriter(rec)
	if err != nil {
		t.Fatal(err)
	}
	run := newRunState(runID, "u1", "default_channel", "", sessionID, conv.ID, "m1", plat, sse)
	if !plat.pending.create(run) {
		t.Fatal("pending create failed")
	}
	plat.setActiveConv(conv.ID, runID)

	go func() {
		time.Sleep(20 * time.Millisecond)
		ctx := context.Background()
		_ = plat.Send(ctx, agentkit.OutboundEvent{
			SessionID: sessionID,
			Type:      agentkit.EventMessageStart,
		})
		delta, _ := json.Marshal(agentkit.MessageUpdatePayload{
			AssistantMessageEvent: agentkit.AssistantMessageEvent{
				Type:  agentkit.AssistantEventTextDelta,
				Delta: "hello",
			},
		})
		_ = plat.Send(ctx, agentkit.OutboundEvent{
			SessionID: sessionID,
			Type:      agentkit.EventMessageUpdate,
			Data:      delta,
		})
		_ = plat.Send(ctx, agentkit.OutboundEvent{
			SessionID: sessionID,
			Type:      agentkit.EventMessageEnd,
		})
	}()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(rec.Body.String(), "text_delta") {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("expected text_delta in %s", rec.Body.String())
}

func TestConversationsList(t *testing.T) {
	p, err := New(Config{}, Deps{})
	if err != nil {
		t.Fatal(err)
	}
	plat := p.(*Platform)
	_, _ = plat.conversations.create("ch1", "user1")

	req := httptest.NewRequest(http.MethodGet, "/v1/conversations?user=u1", nil)
	req.Header.Set("X-Chat-API-Channel", "ch1")
	req.Header.Set("X-Chat-API-User", "u1")
	rec := httptest.NewRecorder()
	plat.handleConversations(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "conversations") {
		t.Fatalf("body %s", rec.Body.String())
	}
}

func TestConversationMessages(t *testing.T) {
	channel := "default_channel"
	conv, err := platWithSessionHistory(channel, "demo", "hello", "world")
	if err != nil {
		t.Fatal(err)
	}
	plat := conv.plat
	convID := conv.id

	req := httptest.NewRequest(http.MethodGet, "/v1/conversations/"+convID+"/messages?limit=10", nil)
	req.Header.Set("X-Chat-API-Channel", channel)
	rec := httptest.NewRecorder()
	plat.handleConversationSub(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		OK   bool `json:"ok"`
		Data struct {
			Messages []map[string]any `json:"messages"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Data.Messages) != 2 {
		t.Fatalf("messages = %d, want 2: %+v", len(resp.Data.Messages), resp.Data.Messages)
	}
	// API returns newest first; assistant reply is the latest entry.
	if resp.Data.Messages[0]["answer"] != "world" {
		t.Fatalf("latest answer = %v", resp.Data.Messages[0]["answer"])
	}
	if resp.Data.Messages[1]["query"] != "hello" {
		t.Fatalf("earlier query = %v", resp.Data.Messages[1]["query"])
	}
}

type testConv struct {
	plat *Platform
	id   string
}

func platWithSessionHistory(channel, user, query, answer string) (*testConv, error) {
	p, err := New(Config{}, Deps{})
	if err != nil {
		return nil, err
	}
	plat := p.(*Platform)
	c, err := plat.conversations.create(channel, user)
	if err != nil {
		return nil, err
	}
	mem, err := session.NewMemory(session.MemoryConfig{
		ID: agentkit.SessionID(engineSessionKey(channel, c.ID)),
	})
	if err != nil {
		return nil, err
	}
	plat.sessionStore = session.NewStaticStore(mem)
	ctx := context.Background()
	if err := session.AppendMessage(ctx, mem, "coder", agentkit.EventUserMessage, agentkit.ModelMessage{
		Role:    "user",
		Content: []agentkit.ContentPart{{Type: "text", Text: query}},
	}); err != nil {
		return nil, err
	}
	if err := session.AppendMessage(ctx, mem, "coder", agentkit.EventAssistantMessage, agentkit.ModelMessage{
		Role:    "assistant",
		Content: []agentkit.ContentPart{{Type: "text", Text: answer}},
	}); err != nil {
		return nil, err
	}
	return &testConv{plat: plat, id: c.ID}, nil
}

func TestToolCallStepDoesNotEndSSEEarly(t *testing.T) {
	p, err := New(Config{ListenAddr: ":0"}, Deps{})
	if err != nil {
		t.Fatal(err)
	}
	plat := p.(*Platform)

	conv, err := plat.conversations.create("default_channel", "u1")
	if err != nil {
		t.Fatal(err)
	}
	sessionID := agentkit.SessionID(engineSessionKey("default_channel", conv.ID))
	rec := httptest.NewRecorder()
	sse, err := newSSEWriter(rec)
	if err != nil {
		t.Fatal(err)
	}
	runID := newRunID()
	run := newRunState(runID, "u1", "default_channel", "", sessionID, conv.ID, "m1", plat, sse)
	if !plat.pending.create(run) {
		t.Fatal("pending create failed")
	}
	plat.setActiveConv(conv.ID, runID)

	ctx := context.Background()
	toolEnd, _ := json.Marshal(agentkit.MessageEndPayload{
		Message: agentkit.ModelMessage{
			Role: "assistant",
			ToolCalls: []agentkit.ToolCall{{
				ID:   "call_1",
				Name: "write",
			}},
		},
	})
	_ = plat.Send(ctx, agentkit.OutboundEvent{
		SessionID: sessionID,
		Type:      agentkit.EventMessageEnd,
		Data:      toolEnd,
	})
	time.Sleep(500 * time.Millisecond)
	if strings.Contains(rec.Body.String(), "event: message_end") {
		t.Fatalf("message_end should not fire before turn/end: %s", rec.Body.String())
	}

	delta, _ := json.Marshal(agentkit.MessageUpdatePayload{
		AssistantMessageEvent: agentkit.AssistantMessageEvent{
			Type:  agentkit.AssistantEventTextDelta,
			Delta: "done",
		},
	})
	_ = plat.Send(ctx, agentkit.OutboundEvent{SessionID: sessionID, Type: agentkit.EventMessageStart})
	_ = plat.Send(ctx, agentkit.OutboundEvent{SessionID: sessionID, Type: agentkit.EventMessageUpdate, Data: delta})
	textEnd, _ := json.Marshal(agentkit.MessageEndPayload{
		Message: agentkit.ModelMessage{
			Role:    "assistant",
			Content: []agentkit.ContentPart{{Type: "text", Text: "done"}},
		},
	})
	_ = plat.Send(ctx, agentkit.OutboundEvent{SessionID: sessionID, Type: agentkit.EventMessageEnd, Data: textEnd})
	turnEnd, _ := json.Marshal(session.TurnEndData{Steps: 2})
	_ = plat.Send(ctx, agentkit.OutboundEvent{SessionID: sessionID, Type: agentkit.EventTurnEnd, Data: turnEnd})

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		body := rec.Body.String()
		if strings.Contains(body, "text_delta") && strings.Contains(body, "event: message_end") {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("expected text_delta then message_end, got: %s", rec.Body.String())
}

func TestSSEWriter(t *testing.T) {
	rec := httptest.NewRecorder()
	sse, err := newSSEWriter(rec)
	if err != nil {
		t.Fatal(err)
	}
	if err := sse.Event("ping", map[string]string{"ok": "1"}); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte("event: ping")) {
		t.Fatalf("body %s", rec.Body.String())
	}
}
