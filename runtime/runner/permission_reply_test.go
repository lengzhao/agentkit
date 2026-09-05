package runner_test

import (
	"context"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/permission"
	rtpermission "github.com/lengzhao/agentkit/runtime/permission"
	"github.com/lengzhao/agentkit/runtime/platform/common"
	"github.com/lengzhao/agentkit/runtime/runner"
	"github.com/lengzhao/agentkit/runtime/session"
)

// permissionReplyPlatform delivers a user turn, then a permission reply routed
// only by delivery session (like Feishu card callbacks).
type permissionReplyPlatform struct {
	mu    sync.Mutex
	stage int

	delivery     agentkit.SessionID
	conversation agentkit.SessionID
	requestID    string
	turnBlocked  chan struct{}
}

func (p *permissionReplyPlatform) Receive(ctx context.Context) (agentkit.MessageEvent, error) {
	p.mu.Lock()
	stage := p.stage
	p.stage++
	p.mu.Unlock()

	switch stage {
	case 0:
		return userEvent(p.delivery, "ask me"), nil
	case 1:
		select {
		case <-p.turnBlocked:
		case <-ctx.Done():
			return agentkit.MessageEvent{}, ctx.Err()
		}
		return common.PermissionReplyEventWithConversation(
			"",
			p.delivery,
			"lark",
			"U1",
			string(p.conversation),
			permission.Reply{
				RequestID: p.requestID,
				UserID:    "U1",
				Text:      "Python",
				Selected:  []int{0},
			},
		), nil
	default:
		return agentkit.MessageEvent{}, io.EOF
	}
}

func (p *permissionReplyPlatform) Send(context.Context, agentkit.OutboundEvent) error {
	return nil
}

type permissionCapturingLoop struct {
	turnBlocked chan struct{}
	releaseTurn chan struct{}
	delivered   chan struct{}

	mu          sync.Mutex
	pendingID   string
	conversation agentkit.SessionID
}

func (l *permissionCapturingLoop) Dispatch(ctx context.Context, req agentkit.LoopRequest) error {
	l.mu.Lock()
	l.conversation = session.ConversationFromLoopRequest(req)
	l.mu.Unlock()

	emit := req.Emit
	go func() {
		close(l.turnBlocked)
		select {
		case <-l.releaseTurn:
		case <-ctx.Done():
		}
		_ = emit(ctx, agentkit.OutboundEvent{
			Type: agentkit.EventPermissionRequest,
			Data: agentkit.MarshalOutboundData(permission.RequestPayload{
				Request: permission.Request{
					ID:   "perm-test",
					Kind: permission.KindQuestion,
					Question: &permission.Question{
						Prompt:  "pick",
						Options: []permission.Option{{Label: "Python"}},
					},
				},
				Conversation: string(l.conversation),
			}),
		})
	}()
	return nil
}

func (l *permissionCapturingLoop) Steer(context.Context, agentkit.ModelMessage) error    { return nil }
func (l *permissionCapturingLoop) Cancel(context.Context, string) error                  { return nil }
func (l *permissionCapturingLoop) FollowUp(context.Context, agentkit.ModelMessage) error { return nil }
func (l *permissionCapturingLoop) IsSessionBusy(agentkit.SessionID) bool                 { return false }

func (l *permissionCapturingLoop) TryDeliverPermission(event agentkit.MessageEvent) bool {
	if len(event.Reply) == 0 {
		return false
	}
	reply, err := rtpermission.DecodeReply(event.Reply)
	if err != nil {
		return false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if session.ConversationFromEvent(event) != l.conversation {
		return false
	}
	if reply.RequestID != "perm-test" {
		return false
	}
	close(l.delivered)
	return true
}

func (l *permissionCapturingLoop) SupersedePendingForInbound(agentkit.MessageEvent) {}

func TestRunnerDeliversPermissionReplyWithConversationOnEnvelope(t *testing.T) {
	t.Parallel()

	delivery := session.BuildDeliverySessionID("lark", "oc_test", "", "U1")
	entry := session.ActiveSessionEntryKey("lark", delivery, session.ScopeChannel, "U1")
	logical := agentkit.SessionID(string(entry) + ":new:20260101")

	turnBlocked := make(chan struct{})
	releaseTurn := make(chan struct{})
	delivered := make(chan struct{})

	loop := &permissionCapturingLoop{
		turnBlocked: turnBlocked,
		releaseTurn: releaseTurn,
		delivered:   delivered,
	}
	platform := &permissionReplyPlatform{
		delivery:     delivery,
		conversation: logical,
		requestID:    "perm-test",
		turnBlocked:  turnBlocked,
	}

	store := mapSessionStore{
		active: map[agentkit.SessionID]agentkit.SessionID{
			entry: logical,
		},
	}

	root, err := runner.New(runner.Config{SessionScope: "channel"}, runner.Deps{
		Platform:     platform,
		Loop:         loop,
		SessionStore: store,
	})
	if err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)
	go func() { done <- root.Run(context.Background(), nil) }()

	select {
	case <-turnBlocked:
	case <-time.After(2 * time.Second):
		t.Fatal("turn did not block on permission")
	}

	select {
	case <-delivered:
	case <-time.After(2 * time.Second):
		t.Fatal("permission reply was not delivered to the logical conversation")
	}

	close(releaseTurn)
}
