package runner

import (
	"context"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/platform/common"
	"github.com/lengzhao/agentkit/runtime/rctx"
	"github.com/lengzhao/agentkit/runtime/session/sessstore"
	rtworkspace "github.com/lengzhao/agentkit/runtime/workspace"
)

func stagedEvent(t *testing.T) agentkit.MessageEvent {
	t.Helper()
	event := common.InboundFromContent("assistant", agentkit.SessionRouteInput{}, "", "read", "", nil, []common.FileAttachment{{Data: []byte("payload"), FileName: "a.txt"}}, nil, nil, common.DeferredInboundOptsFor(rtworkspace.Static(t.TempDir())))
	event, err := common.StageInboundUploads(event)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { common.ReleaseInboundUploads(event) })
	event.Envelope.Conversation = "session"
	if len(event.Uploads) != 1 || event.Uploads[0].Source == nil {
		t.Fatal("not staged")
	}
	return event
}
func assertStagingReleased(t *testing.T, event agentkit.MessageEvent) {
	t.Helper()
	if f, err := event.Uploads[0].Source.Open(); err == nil {
		f.Close()
		t.Fatal("staging file leaked")
	}
}

func TestStoppedSchedulerReleasesQueuedUploads(t *testing.T) {
	first := make(chan struct{})
	release := make(chan struct{})
	s := newScheduler(1, func(_ context.Context, req agentkit.LoopRequest) error { close(first); <-release; return nil }, nil)
	s.submit(context.Background(), agentkit.LoopRequest{Event: agentkit.MessageEvent{Envelope: agentkit.TurnEnvelope{Conversation: "session"}}})
	select {
	case <-first:
	case <-time.After(time.Second):
		t.Fatal("first request not running")
	}
	defer func() { close(release); s.wait(time.Second); s.stop() }()
	event := stagedEvent(t)
	s.submit(context.Background(), agentkit.LoopRequest{Event: event})
	s.stop()
	assertStagingReleased(t, event)
	late := stagedEvent(t)
	s.submit(context.Background(), agentkit.LoopRequest{Event: late})
	assertStagingReleased(t, late)
}

func TestCwdPreparationFailureReleasesUploadStaging(t *testing.T) {
	ws, err := rtworkspace.NewTenant(rtworkspace.TenantConfig{Global: t.TempDir(), LocalBase: t.TempDir(), WorkspaceScope: "global"})
	if err != nil {
		t.Fatal(err)
	}
	mem, _ := sessstore.NewMemory(sessstore.MemoryConfig{ID: "session"})
	store := sessstore.NewStaticStore(mem)
	ctx := rctx.WithConversation(context.Background(), "session")
	if err = store.SetCwdBind(ctx, "session", filepath.Join(t.TempDir(), "gone")); err != nil {
		t.Fatal(err)
	}
	event := stagedEvent(t)
	r := Root{workspace: ws, sessionStore: store}
	if err = r.dispatch(ctx, agentkit.LoopRequest{Event: event}); err == nil {
		t.Fatal("invalid cwd accepted")
	}
	assertStagingReleased(t, event)
}

func TestQueuedUploadsDoNotRetainPayloadsThroughEmitClosure(t *testing.T) {
	ws := rtworkspace.Static(t.TempDir())
	root, err := New(Config{}, Deps{Platform: eofPlatform{}, Loop: &inboundRoutingLoop{busy: map[agentkit.SessionID]bool{"cli:default": true}}, Workspace: ws})
	if err != nil {
		t.Fatal(err)
	}
	r := root.(*Root)
	started, release := make(chan struct{}), make(chan struct{})
	s := newScheduler(1, func(context.Context, agentkit.LoopRequest) error { close(started); <-release; return nil }, nil)
	s.submit(context.Background(), agentkit.LoopRequest{Event: agentkit.MessageEvent{Envelope: agentkit.TurnEnvelope{Conversation: "cli:default"}}})
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("worker did not start")
	}
	defer func() { s.stop(); close(release); s.wait(time.Second) }()
	payload := make([]byte, 1<<20)
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	enqueue := func() {
		for range 32 {
			event := common.InboundFromContent("assistant", agentkit.SessionRouteInput{Platform: "cli", ChannelID: "default"}, "U1", "read", "", nil, []common.FileAttachment{{Data: payload, FileName: "a.bin"}}, nil, nil, common.DeferredInboundOptsFor(ws))
			r.handleInbound(context.Background(), s, event)
		}
	}
	enqueue()
	runtime.GC()
	runtime.ReadMemStats(&after)
	growth := int64(after.HeapAlloc) - int64(before.HeapAlloc)
	if growth > 16<<20 {
		t.Fatalf("32 queued MiB retained %0.2f MiB heap; an event or emit closure still owns payloads", float64(growth)/(1<<20))
	}
	t.Logf("32 queued MiB retained %0.2f MiB heap", float64(growth)/(1<<20))
	runtime.KeepAlive(payload)
	runtime.KeepAlive(s)
}

func TestCanceledSubmissionReleasesStaging(t *testing.T) {
	event := stagedEvent(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s := newScheduler(1, func(context.Context, agentkit.LoopRequest) error { t.Error("canceled request dispatched"); return nil }, nil)
	defer s.stop()
	s.submit(ctx, agentkit.LoopRequest{Event: event})
	s.wait(time.Second)
	assertStagingReleased(t, event)
}
