package slack

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/interaction"
	"github.com/lengzhao/agentkit/runtime/platform/common"
	"github.com/lengzhao/agentkit/runtime/rctx"
	slackapi "github.com/slack-go/slack"
)

type testInteractionHandler struct{ events chan interaction.Event }

func (h *testInteractionHandler) HandleInteraction(_ context.Context, e interaction.Event) (bool, error) {
	h.events <- e
	return true, nil
}
func TestUnknownSlackCommandKeepsQuestionAndAnswer(t *testing.T) {
	h := &testInteractionHandler{events: make(chan interaction.Event, 5)}
	p := &Platform{dedup: &common.MessageDedup{}, interactionHandlers: []interaction.Handler{h}}
	cb := slackapi.InteractionCallback{Type: slackapi.InteractionTypeBlockActions, User: slackapi.User{ID: "U1"}, Channel: slackapi.Channel{GroupConversation: slackapi.GroupConversation{Conversation: slackapi.Conversation{ID: "C1"}}}}
	cb.Message = slackapi.NewBlockMessage(slackapi.NewSectionBlock(slackapi.NewTextBlockObject(slackapi.MarkdownType, "请选择部署环境", false, false), nil, nil))
	cb.Message.Timestamp = "1.0"
	cb.Message.ThreadTimestamp = "0.1"
	action := &slackapi.BlockAction{ActionID: "env", Value: "cmd:/external", SelectedOptions: []slackapi.OptionBlockObject{{Value: "prod", Text: slackapi.NewTextBlockObject(slackapi.PlainTextType, "生产", false, false)}}}
	p.handleBlockAction(cb, action)
	select {
	case e := <-h.events:
		if e.ThreadID != "0.1" || !strings.Contains(e.CardText, "请选择部署环境") || !strings.Contains(string(e.RawAction), "prod") {
			t.Fatalf("incomplete event: %+v", e)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("missing event")
	}
}
func TestSlackOperationIncludesInputAndForm(t *testing.T) {
	var cb slackapi.InteractionCallback
	if err := json.Unmarshal([]byte(`{"type":"block_actions","user":{"id":"U1"},"channel":{"id":"C1"},"state":{"values":{"b":{"comment":{"type":"plain_text_input","value":"hello"}}}}}`), &cb); err != nil {
		t.Fatal(err)
	}
	a := &slackapi.BlockAction{ActionID: "env", SelectedOptions: []slackapi.OptionBlockObject{{Value: "prod", Text: slackapi.NewTextBlockObject(slackapi.PlainTextType, "生产", false, false)}}}
	e := slackInteractionSnapshot(cb, a, "s", "C1", "1", "", "select")
	raw, _ := json.Marshal(e)
	for _, s := range []string{"hello", "prod", "生产"} {
		if !strings.Contains(string(raw), s) {
			t.Fatal(string(raw))
		}
	}
}

func TestSlackChangedFormAnswersAreNotDeduped(t *testing.T) {
	h := &testInteractionHandler{events: make(chan interaction.Event, 4)}
	p := &Platform{interactionHandlers: []interaction.Handler{h}}
	var cb slackapi.InteractionCallback
	_ = json.Unmarshal([]byte(`{"type":"block_actions","user":{"id":"U1"},"channel":{"id":"C1"},"state":{"values":{"b":{"comment":{"type":"plain_text_input","value":"first"}}}}}`), &cb)
	cb.Message = slackapi.NewBlockMessage(slackapi.NewSectionBlock(slackapi.NewTextBlockObject(slackapi.MarkdownType, "question", false, false), nil, nil))
	cb.Message.Timestamp = "1"
	a := &slackapi.BlockAction{ActionID: "submit", Value: "send"}
	p.handleBlockAction(cb, a)
	p.handleBlockAction(cb, a)
	v := cb.BlockActionState.Values["b"]["comment"]
	v.Value = "second"
	cb.BlockActionState.Values["b"]["comment"] = v
	p.handleBlockAction(cb, a)
	for i := 0; i < 2; i++ {
		select {
		case <-h.events:
		case <-time.After(time.Second):
			t.Fatal("different form suppressed")
		}
	}
	select {
	case <-h.events:
		t.Fatal("duplicate forwarded")
	case <-time.After(50 * time.Millisecond):
	}
}
func TestSlackQueryUsesExactThreadMessage(t *testing.T) {
	paths := make(chan string, 4)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		paths <- r.URL.Path
		_ = r.ParseForm()
		if r.Form.Get("ts") != "root" {
			t.Errorf("wrong root: %v", r.Form)
		}
		_, _ = w.Write([]byte(`{"ok":true,"messages":[{"ts":"other","text":"wrong question"}]}`))
	}))
	defer srv.Close()
	h := &testInteractionHandler{events: make(chan interaction.Event, 4)}
	p := &Platform{client: slackapi.New("token", slackapi.OptionAPIURL(srv.URL+"/")), interactionHandlers: []interaction.Handler{h}}
	cb := slackapi.InteractionCallback{Type: slackapi.InteractionTypeBlockActions, User: slackapi.User{ID: "U1"}}
	e := slackInteractionSnapshot(cb, &slackapi.BlockAction{Value: "answer"}, "slack:C1:t:root:u:U1", "C1", "target", "root", "answer")
	p.processBlockInteraction(context.Background(), e)
	select {
	case got := <-h.events:
		if got.CardStatus != interaction.CardUnavailable || got.CardText != "" {
			t.Fatalf("wrong message used: %+v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("missing event")
	}
	if path := <-paths; !strings.HasSuffix(path, "conversations.replies") {
		t.Fatal(path)
	}
}
func TestSlackQueryFailureStillEnqueuesAnswer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":false,"error":"missing_scope"}`))
	}))
	defer srv.Close()
	p := &Platform{client: slackapi.New("token", slackapi.OptionAPIURL(srv.URL+"/")), inbox: common.NewInbox(4)}
	cb := slackapi.InteractionCallback{Type: slackapi.InteractionTypeBlockActions, User: slackapi.User{ID: "U1"}}
	e := slackInteractionSnapshot(cb, &slackapi.BlockAction{ActionID: "answer", Value: "my answer"}, p.buildSessionKey("C1", "U1", ""), "C1", "target", "", "submit")
	p.processBlockInteraction(context.Background(), e)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	msg, err := p.inbox.Receive(ctx)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(msg.Message)
	for _, s := range []string{"failed", "missing_scope", "my answer", "target", "raw_callback"} {
		if !strings.Contains(string(raw), s) {
			t.Errorf("missing %s: %s", s, raw)
		}
	}
}

func TestSlackRejectsConflictingThreadSession(t *testing.T) {
	h := &testInteractionHandler{events: make(chan interaction.Event, 2)}
	p := &Platform{interactionHandlers: []interaction.Handler{h}}
	var cb slackapi.InteractionCallback
	_ = json.Unmarshal([]byte(`{"type":"block_actions","user":{"id":"U1"},"channel":{"id":"C1"},"message":{"ts":"2","thread_ts":"threadB","text":"question"}}`), &cb)
	a := &slackapi.BlockAction{ActionID: "answer", Value: encodeActionValue("external", p.buildSessionKey("C1", "U1", "threadA"), nil)}
	p.handleBlockAction(cb, a)
	select {
	case e := <-h.events:
		if e.SessionID != p.buildSessionKey("C1", "U1", "threadB") {
			t.Fatalf("cross-topic route: %+v", e)
		}
	case <-time.After(time.Second):
		t.Fatal("missing event")
	}
}
func TestSlackModalControlUsesViewContext(t *testing.T) {
	h := &testInteractionHandler{events: make(chan interaction.Event, 2)}
	p := &Platform{interactionHandlers: []interaction.Handler{h}}
	var cb slackapi.InteractionCallback
	_ = json.Unmarshal([]byte(`{"type":"block_actions","user":{"id":"U1"},"view":{"id":"V1","private_metadata":"{\"session_key\":\"slack:C1:t:root:u:U1\"}","title":{"type":"plain_text","text":"环境问题"},"blocks":[]}}`), &cb)
	p.handleBlockAction(cb, &slackapi.BlockAction{ActionID: "env", SelectedOption: slackapi.OptionBlockObject{Value: "prod"}})
	select {
	case e := <-h.events:
		if e.ChatID != "C1" || e.ThreadID != "root" || e.CardStatus != interaction.CardAvailable || !strings.Contains(e.CardText, "环境问题") {
			t.Fatalf("modal context lost: %+v", e)
		}
	case <-time.After(time.Second):
		t.Fatal("missing event")
	}
}

func TestSlackSnapshotRetainsUnmodeledPayload(t *testing.T) {
	raw := json.RawMessage(`{"type":"block_actions","user":{"id":"U1"},"channel":{"id":"C1"},"actions":[{"action_id":"a","block_id":"b","type":"button","value":"yes","future_answer":"extra"}],"message":{"ts":"1","text":"question","future_card":"extra"},"future_callback":"extra"}`)
	var cb slackapi.InteractionCallback
	if err := json.Unmarshal(raw, &cb); err != nil {
		t.Fatal(err)
	}
	e := slackInteractionSnapshot(cb, cb.ActionCallback.BlockActions[0], "s", "C1", "1", "", "yes", raw)
	if !strings.Contains(string(e.RawCallback), "future_callback") || !strings.Contains(string(e.RawAction), "future_answer") || !strings.Contains(string(e.RawCard), "future_card") {
		t.Fatal("unmodeled fields lost")
	}
}
func TestSlackModalSubmissionUsesDistinctPrivateSessions(t *testing.T) {
	p := &Platform{}
	a := slackapi.InteractionCallback{User: slackapi.User{ID: "U1"}, View: slackapi.View{ID: "V1"}}
	b := a
	b.View.ID = "V2"
	sa, ca, _ := p.slackViewRoute(a)
	sb, cb, _ := p.slackViewRoute(b)
	if sa == sb || ca != "U1" || cb != "U1" {
		t.Fatal("view isolation lost")
	}
	da, _ := parseSessionKey(sa)
	db, _ := parseSessionKey(sb)
	if da.threadTS == db.threadTS {
		t.Fatal("view IDs were truncated by session parser")
	}
}

func TestSlackPreservesOriginalFlatSessionOnThreadReplyCard(t *testing.T) {
	h := &testInteractionHandler{events: make(chan interaction.Event, 2)}
	p := &Platform{interactionHandlers: []interaction.Handler{h}}
	var cb slackapi.InteractionCallback
	_ = json.Unmarshal([]byte(`{"type":"block_actions","user":{"id":"U1"},"channel":{"id":"C1"},"message":{"ts":"2","thread_ts":"replyThread","text":"question"}}`), &cb)
	session := p.buildSessionKey("C1", "U1", "")
	p.handleBlockAction(cb, &slackapi.BlockAction{ActionID: "a", Value: encodeActionValue("answer", session, nil)})
	select {
	case e := <-h.events:
		if e.SessionID != session || e.ThreadID != "replyThread" {
			t.Fatal("original conversation lost")
		}
	case <-time.After(time.Second):
		t.Fatal("missing event")
	}
}
func TestSlackPrivateModalDeliveryDoesNotUseViewAsThreadTS(t *testing.T) {
	d := interactionDelivery(interaction.Event{InteractionID: "V1", ChatID: "U1", UserID: "U1", SessionID: "slack:U1:t:view-V1:u:U1", ThreadID: "view-V1"})
	if !d.directMessage || d.threadTS != "" {
		t.Fatalf("invalid modal delivery: %+v", d)
	}
}

func TestInteractionQueueWaitOutlivesWorkerDeadline(t *testing.T) {
	p := &Platform{inbox: common.NewInbox(1)}
	p.userProfileCache.Store("U1", slackUserProfileEntry{})
	_ = p.inbox.Push(context.Background(), agentkit.MessageEvent{UserID: "sentinel"})
	e := interaction.Event{Platform: "slack", Type: "block_actions", UserID: "U1", ChatID: "C1", MessageID: "m", SessionID: p.buildSessionKey("C1", "U1", ""), CardStatus: interaction.CardAvailable, RawCard: json.RawMessage(`{"text":"question"}`), Answers: []interaction.Answer{{Value: "answer"}}}
	workerCtx, cancel := context.WithCancel(context.Background())
	cancel()
	done := make(chan error, 1)
	go func() { done <- p.processBlockInteraction(workerCtx, e) }()
	select {
	case err := <-done:
		t.Fatalf("delivery ended before queue space became available: %v", err)
	case <-time.After(30 * time.Millisecond):
	}
	_, _ = p.inbox.Receive(context.Background())
	ctx, stop := context.WithTimeout(context.Background(), time.Second)
	defer stop()
	msg, err := p.inbox.Receive(ctx)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(msg.Message)
	if !strings.Contains(string(raw), "answer") {
		t.Fatal("answer was lost")
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("delivery did not complete")
	}
}

type profileMetadataCommands struct{ seen chan map[string]any }

func (c *profileMetadataCommands) Dispatch(ctx context.Context, _ string, _ string) (string, error) {
	c.seen <- rctx.MetadataFromContext(ctx)
	return "", nil
}
func (c *profileMetadataCommands) List(context.Context) []agentkit.Command { return nil }
func TestSlackCardCommandPreservesProfileMetadata(t *testing.T) {
	c := &profileMetadataCommands{seen: make(chan map[string]any, 1)}
	p := &Platform{commands: c}
	p.userProfileCache.Store("U1", slackUserProfileEntry{name: "Alice", email: "alice@example.com", ok: true})
	p.userProfileCache.Store("U2", slackUserProfileEntry{name: "Bob", ok: true})
	var cb slackapi.InteractionCallback
	_ = json.Unmarshal([]byte(`{"type":"block_actions","user":{"id":"U1"},"channel":{"id":"C1"},"message":{"ts":"1","text":"question"}}`), &cb)
	p.handleBlockAction(cb, &slackapi.BlockAction{ActionID: "me", Value: "cmd:/me <@U2>"})
	select {
	case md := <-c.seen:
		if md["displayName"] != "Alice" || md["email"] != "alice@example.com" || md["mentions"] == nil {
			t.Fatalf("metadata lost: %v", md)
		}
	case <-time.After(time.Second):
		t.Fatal("command not called")
	}
}

func TestSlackInteractionProfileLookupHonorsContext(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	defer srv.Close()
	defer close(release)
	p := &Platform{client: slackapi.New("token", slackapi.OptionAPIURL(srv.URL+"/"))}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	done := make(chan map[string]any, 1)
	go func() { done <- p.userProfileMetadataContext(ctx, "U1") }()
	select {
	case md := <-done:
		if len(md) != 0 {
			t.Fatal(md)
		}
	case <-time.After(time.Second):
		t.Fatal("profile lookup ignored its context")
	}
}
