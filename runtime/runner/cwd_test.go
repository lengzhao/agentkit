package runner

import (
	"context"
	"github.com/lengzhao/agentkit/runtime/platform/common"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/workspace"
	"github.com/lengzhao/agentkit/runtime/rctx"
	"github.com/lengzhao/agentkit/runtime/session/sessstore"
	rtworkspace "github.com/lengzhao/agentkit/runtime/workspace"
)

type cwdLoop struct {
	agentkit.Loop
	seen chan string
}

func (l cwdLoop) Dispatch(ctx context.Context, _ agentkit.LoopRequest) error {
	l.seen <- workspace.WorkingDirectory(ctx)
	return nil
}

func TestRunnerPreparesCwdForAnyLoop(t *testing.T) {
	root := t.TempDir()
	a := t.TempDir()
	b := t.TempDir()
	ws := rtworkspace.Static(root)
	store, err := sessstore.NewStore(sessstore.StoreConfig{Dir: "sessions"}, sessstore.StoreDeps{Workspace: ws})
	if err != nil {
		t.Fatal(err)
	}
	seen := make(chan string, 2)
	r := Root{loop: cwdLoop{seen: seen}, workspace: ws, sessionStore: store}
	var cmd agentkit.Command
	for _, c := range r.Commands() {
		if c.Name() == "cwd" {
			cmd = c
		}
	}
	if cmd == nil {
		t.Fatal("custom loop runner has no /cwd command")
	}
	for _, item := range []struct{ id, path string }{{"one", a}, {"two", b}} {
		ctx := rctx.WithConversation(context.Background(), item.id)
		if _, err := cmd.CommandExec(ctx, "use "+item.path); err != nil {
			t.Fatal(err)
		}
	}
	done := make(chan error, 2)
	for _, id := range []string{"one", "two"} {
		go func(id string) {
			done <- r.dispatch(context.Background(), agentkit.LoopRequest{Event: agentkit.MessageEvent{Envelope: agentkit.TurnEnvelope{Conversation: id}}})
		}(id)
	}
	for range 2 {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	got := map[string]bool{<-seen: true, <-seen: true}
	if !got[a] || !got[b] {
		t.Fatalf("concurrent cwd snapshots: %v", got)
	}
}

// The upload arrives before the binding changes; it must use the binding at
// dispatch, and the model path must continue to work after another cwd change.
func TestRunnerMaterializesUploadsAfterCwdResolution(t *testing.T) {
	ws := rtworkspace.Static(t.TempDir())
	first, second := t.TempDir(), t.TempDir()
	store, err := sessstore.NewStore(sessstore.StoreConfig{Dir: "sessions"}, sessstore.StoreDeps{Workspace: ws})
	if err != nil {
		t.Fatal(err)
	}
	ctx := rctx.WithConversation(context.Background(), "session")
	if err = store.(agentkit.SessionCwdStore).SetCwdBind(ctx, "session", first); err != nil {
		t.Fatal(err)
	}
	event := common.InboundFromContent("assistant", agentkit.SessionRouteInput{}, "U1", "read this", "", nil, []common.FileAttachment{{FileName: "a.txt", Data: []byte("payload")}}, nil, nil, common.DeferredInboundOptsFor(ws))
	event.Envelope.Conversation = "session"
	if err = store.(agentkit.SessionCwdStore).SetCwdBind(ctx, "session", second); err != nil {
		t.Fatal(err)
	}
	seen := make(chan agentkit.MessageEvent, 1)
	r := Root{loop: uploadLoop{seen: seen}, workspace: ws, sessionStore: store}
	if err = r.dispatch(context.Background(), agentkit.LoopRequest{Event: event}); err != nil {
		t.Fatal(err)
	}
	got := <-seen
	if len(got.Uploads) != 0 || len(got.Attachments) != 1 {
		t.Fatalf("unprepared event: %#v", got)
	}
	want := filepath.Join(second, "upload", "a.txt")
	if got.Attachments[0].Path != want {
		t.Fatalf("path = %s, want %s", got.Attachments[0].Path, want)
	}
	data, err := os.ReadFile(want)
	if err != nil || string(data) != "payload" {
		t.Fatalf("payload = %q, %v", data, err)
	}
	if !strings.Contains(got.Message.Content[0].Text, want) {
		t.Fatal("model reference missing")
	}
}

type uploadLoop struct {
	agentkit.Loop
	seen chan agentkit.MessageEvent
}

func (l uploadLoop) Dispatch(ctx context.Context, req agentkit.LoopRequest) error {
	l.seen <- req.Event
	return nil
}

func TestRunnerInjectedMetadataPreservesImageUploadReference(t *testing.T) {
	ws := rtworkspace.Static(t.TempDir())
	event := common.InboundFromContent("assistant", agentkit.SessionRouteInput{Platform: "lark", ChannelID: "C1"}, "U1", "", "", []common.ImageAttachment{{Data: []byte("image"), FileName: "pic.png", MimeType: "image/png"}}, nil, nil, nil, common.DeferredInboundOptsFor(ws))
	event.Envelope.Conversation = "session"
	seen := make(chan agentkit.MessageEvent, 1)
	r := Root{loop: uploadLoop{seen: seen}, workspace: ws, inject: []string{"sender_id"}}
	event = r.formatInboundEvent(event, event.Envelope)
	if err := r.dispatch(context.Background(), agentkit.LoopRequest{Event: event}); err != nil {
		t.Fatal(err)
	}
	got := <-seen
	if len(got.Message.Content) != 2 || got.Message.Content[1].Source == "" {
		t.Fatalf("prepared image lost: %#v", got.Message)
	}
	if !strings.Contains(got.Message.Content[0].Text, "sender_id") {
		t.Fatal("metadata lost")
	}
}

func TestRunnerWithoutWorkspaceUsesIngressWorkspaceForUploads(t *testing.T) {
	ws := rtworkspace.Static(t.TempDir())
	mem, _ := sessstore.NewMemory(sessstore.MemoryConfig{ID: "session"})
	store := sessstore.NewStaticStore(mem)
	cwd := t.TempDir()
	if err := store.SetCwdBind(context.Background(), "session", cwd); err != nil {
		t.Fatal(err)
	}
	event := common.InboundFromContent("assistant", agentkit.SessionRouteInput{}, "", "read", "", nil, []common.FileAttachment{{Data: []byte("a"), FileName: "a.txt"}}, nil, nil, common.DeferredInboundOptsFor(ws))
	event.Envelope.Conversation = "session"
	seen := make(chan agentkit.MessageEvent, 1)
	r := Root{loop: uploadLoop{seen: seen}, sessionStore: store}
	if err := r.dispatch(context.Background(), agentkit.LoopRequest{Event: event}); err != nil {
		t.Fatal(err)
	}
	got := <-seen
	if len(got.Attachments) != 1 || got.Attachments[0].Path != filepath.Join(cwd, "upload", "a.txt") {
		t.Fatalf("missing ingress workspace fallback: %#v", got.Attachments)
	}
}
