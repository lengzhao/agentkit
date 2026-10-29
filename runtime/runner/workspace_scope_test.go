package runner

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/platform/common"
	"github.com/lengzhao/agentkit/runtime/rctx"
	"github.com/lengzhao/agentkit/runtime/session/sessstore"
	rtworkspace "github.com/lengzhao/agentkit/runtime/workspace"
)

type scopeCaptureLoop struct {
	inboundRoutingLoop
	seen chan agentkit.MessageEvent
}

func (l *scopeCaptureLoop) Dispatch(_ context.Context, req agentkit.LoopRequest) error {
	l.seen <- req.Event
	return nil
}

func TestRoutedUploadsFollowThreadAndActiveSessionPolicies(t *testing.T) {
	for _, scope := range []string{"thread", "session"} {
		t.Run(scope, func(t *testing.T) {
			ws, err := rtworkspace.NewTenant(rtworkspace.TenantConfig{Global: t.TempDir(), LocalBase: t.TempDir(), WorkspaceScope: scope})
			if err != nil {
				t.Fatal(err)
			}
			store, err := sessstore.NewStore(sessstore.StoreConfig{Dir: "sessions"}, sessstore.StoreDeps{Workspace: ws})
			if err != nil {
				t.Fatal(err)
			}
			l := &scopeCaptureLoop{seen: make(chan agentkit.MessageEvent, 1)}
			r := Root{platform: eofPlatform{}, loop: l, sessionStore: store, workspace: ws, sessionScope: agentkit.SessionScopeThread}
			errors := make(chan error, 1)
			sched := newScheduler(1, r.dispatch, func(_ context.Context, _ agentkit.LoopRequest, err error) { errors <- err })
			defer sched.stop()
			send := func(thread string) agentkit.MessageEvent {
				t.Helper()
				event := common.InboundFromContent("assistant", agentkit.SessionRouteInput{Platform: "lark", Instance: "bot1", ChannelID: "C1", ThreadID: thread}, "U1", "read", "", nil, []common.FileAttachment{{FileName: "a.txt", Data: []byte(thread)}}, nil, nil, common.DeferredInboundOptsFor(ws))
				r.handleInbound(context.Background(), sched, event)
				select {
				case got := <-l.seen:
					sched.wait(time.Second)
					return got
				case err := <-errors:
					t.Fatal(err)
				case <-time.After(3 * time.Second):
					t.Fatal("routed upload not dispatched")
				}
				return agentkit.MessageEvent{}
			}
			first := send("T1")
			other := send("T2")
			dir := func(event agentkit.MessageEvent) string {
				t.Helper()
				if len(event.Uploads) != 0 || len(event.Attachments) != 1 {
					t.Fatalf("unprepared event: %#v", event)
				}
				return filepath.Dir(event.Attachments[0].Path)
			}
			original := dir(first)
			if dir(other) == original {
				t.Fatal("independent threads share directory")
			}
			ctx := rctx.ApplyEnvelopeToContext(context.Background(), first.Envelope)
			entry := rctx.ActiveEntryKey(first.Envelope.Route, r.routePolicy(first), "U1")
			active := store.(agentkit.ActiveSessionStore)
			if err = active.SetActiveSession(ctx, entry, "new-logical-session"); err != nil {
				t.Fatal(err)
			}
			switched := send("T1")
			if switched.Envelope.Conversation != "new-logical-session" {
				t.Fatal("active session not applied")
			}
			if (dir(switched) == original) != (scope == "thread") {
				t.Fatalf("%s policy directory after /new: %s original %s", scope, dir(switched), original)
			}
			if err = active.SetActiveSession(ctx, entry, agentkit.SessionID(first.Envelope.Conversation)); err != nil {
				t.Fatal(err)
			}
			if got := dir(send("T1")); got != original {
				t.Fatalf("restore lost workspace: %s != %s", got, original)
			}
		})
	}
}
