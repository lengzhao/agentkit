package runner

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lengzhao/agentkit"
	capsession "github.com/lengzhao/agentkit/cap/session"
	"github.com/lengzhao/agentkit/runtime/loop"
	"github.com/lengzhao/agentkit/runtime/rctx"
)

type eofPlatform struct{}

func (eofPlatform) Receive(context.Context) (agentkit.MessageEvent, error) {
	return agentkit.MessageEvent{}, io.EOF
}
func (eofPlatform) Send(context.Context, agentkit.OutboundEvent) error { return nil }

type inboundRoutingLoop struct {
	mu        sync.Mutex
	busy      map[agentkit.SessionID]bool
	followUps []string
	steers    []string
}

func (l *inboundRoutingLoop) Dispatch(context.Context, agentkit.LoopRequest) error { return nil }
func (l *inboundRoutingLoop) Cancel(context.Context, string) error                 { return nil }
func (l *inboundRoutingLoop) CancelAllInFlight(string)                             {}
func (l *inboundRoutingLoop) TryDeliverPermission(agentkit.MessageEvent) bool      { return false }
func (l *inboundRoutingLoop) SupersedePendingForInbound(agentkit.MessageEvent)     {}

func (l *inboundRoutingLoop) Steer(_ context.Context, msg agentkit.ModelMessage) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.steers = append(l.steers, textPart(msg))
	return nil
}

func (l *inboundRoutingLoop) FollowUp(_ context.Context, msg agentkit.ModelMessage) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.followUps = append(l.followUps, textPart(msg))
	return nil
}

func (l *inboundRoutingLoop) IsSessionBusy(id agentkit.SessionID) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.busy[id]
}

func textPart(msg agentkit.ModelMessage) string {
	for _, part := range msg.Content {
		if part.Type == "text" {
			return part.Text
		}
	}
	return ""
}

func subagentCompleteEvent(sessionID agentkit.SessionID) agentkit.MessageEvent {
	return agentkit.MessageEvent{

		Envelope: agentkit.TurnEnvelope{
			Route: rctx.SessionRoute("cli", string(sessionID)).WithInstance("cli"), Metadata: map[string]any{
				"subagent_complete": true,
			}},
		Message: agentkit.ModelMessage{
			Role:    "user",
			Content: []agentkit.ContentPart{{Type: "text", Text: "[subagent-complete agent=cursor job=sub:1 status=failed]\nerr"}},
		},
	}
}

func TestSubagentCompleteWhileBusyQueuesNewTurn(t *testing.T) {
	t.Parallel()

	sessionID := agentkit.SessionID("cli:default")
	loop := &inboundRoutingLoop{
		busy: map[agentkit.SessionID]bool{sessionID: true},
	}
	root, err := New(Config{}, Deps{Platform: eofPlatform{}, Loop: loop})
	if err != nil {
		t.Fatal(err)
	}
	r := root.(*Root)

	var submitted []agentkit.LoopRequest
	var submitMu sync.Mutex
	sched := newScheduler(1, func(_ context.Context, req agentkit.LoopRequest) error {
		submitMu.Lock()
		submitted = append(submitted, req)
		submitMu.Unlock()
		return nil
	}, nil)

	r.handleInbound(context.Background(), sched, subagentCompleteEvent(sessionID))

	// Async subagent completion must NOT steer into the running turn and must NOT
	// use the in-loop FollowUp queue; it is a new logical turn routed through the
	// scheduler's per-session pending queue (runs as the next Run after the
	// current turn ends). The scheduler dispatch is async, so wait for it.
	deadline := time.Now().Add(2 * time.Second)
	for {
		submitMu.Lock()
		n := len(submitted)
		submitMu.Unlock()
		if n >= 1 || time.Now().After(deadline) {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	loop.mu.Lock()
	defer loop.mu.Unlock()
	if len(loop.steers) != 0 {
		t.Fatalf("steer = %v, want none", loop.steers)
	}
	if len(loop.followUps) != 0 {
		t.Fatalf("follow-ups = %v, want none (subagent-complete routes via scheduler)", loop.followUps)
	}
	submitMu.Lock()
	defer submitMu.Unlock()
	if len(submitted) != 1 {
		t.Fatalf("submitted turns = %d, want 1", len(submitted))
	}
	if !strings.Contains(textPart(submitted[0].Event.Message), "subagent-complete") {
		t.Fatalf("submitted turn = %q, want subagent-complete", textPart(submitted[0].Event.Message))
	}
}

func TestBusyUserMessageStillSteers(t *testing.T) {
	t.Parallel()

	sessionID := agentkit.SessionID("cli:default")
	loop := &inboundRoutingLoop{
		busy: map[agentkit.SessionID]bool{sessionID: true},
	}
	root, err := New(Config{}, Deps{Platform: eofPlatform{}, Loop: loop})
	if err != nil {
		t.Fatal(err)
	}
	r := root.(*Root)
	sched := newScheduler(1, func(context.Context, agentkit.LoopRequest) error { return nil }, nil)

	r.handleInbound(context.Background(), sched, agentkit.MessageEvent{

		Envelope: agentkit.TurnEnvelope{
			Route: rctx.SessionRoute("cli", string(sessionID)).WithInstance("cli"),
		},
		Message: agentkit.ModelMessage{
			Role:    "user",
			Content: []agentkit.ContentPart{{Type: "text", Text: "interrupt"}},
		},
	})

	loop.mu.Lock()
	defer loop.mu.Unlock()
	if len(loop.steers) != 1 {
		t.Fatalf("steers = %d, want 1", len(loop.steers))
	}
	if len(loop.followUps) != 0 {
		t.Fatalf("follow-ups = %v, want none", loop.followUps)
	}
}

func TestBusyQueuedUserMessageStartsNewTurn(t *testing.T) {
	sessionID := agentkit.SessionID("cli:default")
	loop := &inboundRoutingLoop{busy: map[agentkit.SessionID]bool{sessionID: true}}
	root, err := New(Config{}, Deps{Platform: eofPlatform{}, Loop: loop})
	if err != nil {
		t.Fatal(err)
	}
	submitted := make(chan agentkit.LoopRequest, 1)
	sched := newScheduler(1, func(_ context.Context, req agentkit.LoopRequest) error {
		submitted <- req
		return nil
	}, nil)
	defer sched.stop()
	defer sched.wait(2 * time.Second)
	event := agentkit.MessageEvent{

		Envelope: agentkit.TurnEnvelope{Route: rctx.SessionRoute("cli", string(sessionID)).WithInstance("cli"), Metadata: map[string]any{agentkit.MetadataQueueTurn: true}},
		Message:  agentkit.ModelMessage{Role: "user", Content: []agentkit.ContentPart{{Type: "text", Text: "next turn"}}},
	}
	root.(*Root).handleInbound(context.Background(), sched, event)
	loop.mu.Lock()
	steers := len(loop.steers)
	loop.mu.Unlock()
	if steers != 0 {
		t.Fatalf("queued input was steered into finishing turn: %d", steers)
	}
	select {
	case req := <-submitted:
		if !strings.Contains(textPart(req.Event.Message), "next turn") {
			t.Fatalf("queued message = %v", req.Event.Message)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("queued input did not start a new turn")
	}
}

type recordingPlatform struct {
	mu     sync.Mutex
	events []agentkit.OutboundEvent
}

func (p *recordingPlatform) Receive(context.Context) (agentkit.MessageEvent, error) {
	return agentkit.MessageEvent{}, io.EOF
}

func (p *recordingPlatform) Send(_ context.Context, event agentkit.OutboundEvent) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.events = append(p.events, event)
	return nil
}

func (p *recordingPlatform) steerEvents() []agentkit.OutboundEvent {
	p.mu.Lock()
	defer p.mu.Unlock()
	var out []agentkit.OutboundEvent
	for _, ev := range p.events {
		if ev.Type == agentkit.EventTurnSteer {
			out = append(out, ev)
		}
	}
	return out
}

// A busy-session message is answered with a turn/steer event carrying the
// steered message's own route, so the platform can mark that exact message.
func TestBusyUserMessageEmitsTurnSteerEvent(t *testing.T) {
	t.Parallel()

	sessionID := agentkit.SessionID("lark:oc_1")
	loop := &inboundRoutingLoop{busy: map[agentkit.SessionID]bool{sessionID: true}}
	platform := &recordingPlatform{}
	root, err := New(Config{}, Deps{Platform: platform, Loop: loop})
	if err != nil {
		t.Fatal(err)
	}
	platformID := "lark"
	r := root.(*Root)
	sched := newScheduler(1, func(context.Context, agentkit.LoopRequest) error { return nil }, nil)
	defer sched.stop()
	defer sched.wait(2 * time.Second)

	route := agentkit.SessionRouteInput{
		Platform:    platformID,
		DeliveryID:  sessionID,
		ChannelID:   "oc_1",
		ReplyTo:     "om_steered",
		ScopeUserID: "ou_1",
	}
	event := agentkit.MessageEvent{

		Envelope: agentkit.TurnEnvelope{Route: rctx.BuildSessionRoute(route).WithInstance(platformID), Actor: agentkit.ActorRef{UserID: "ou_1"}, AgentID: "assistant"},
		Message: agentkit.ModelMessage{
			Role:    "user",
			Content: []agentkit.ContentPart{{Type: "text", Text: "改一下方向"}},
		},
	}
	// handleInbound resolves the conversation from the delivery route.
	r.handleInbound(context.Background(), sched, event)

	loop.mu.Lock()
	steers := append([]string(nil), loop.steers...)
	loop.mu.Unlock()
	if len(steers) != 1 {
		t.Fatalf("steers = %v, want the busy message steered", loop.steers)
	}

	events := platform.steerEvents()
	if len(events) != 1 {
		t.Fatalf("turn/steer events = %d, want 1", len(events))
	}
	got := events[0]
	if replyTo := rctx.RouteReplyTo(got.Route); replyTo != "om_steered" {
		t.Fatalf("steer event reply anchor = %q, want om_steered", replyTo)
	}
	if delivery, ok := rctx.RouteSessionID(got.Route); !ok || delivery != sessionID {
		t.Fatalf("steer event delivery = %q, want %q", delivery, sessionID)
	}
	var data capsession.TurnSteerData
	if err := json.Unmarshal(got.Data, &data); err != nil {
		t.Fatalf("decode turn/steer payload: %v", err)
	}
	if data.Outcome != capsession.SteerOutcomeQueued {
		t.Fatalf("outcome = %q, want %q", data.Outcome, capsession.SteerOutcomeQueued)
	}
	if data.UserID != "ou_1" || data.AgentID != "assistant" {
		t.Fatalf("steer data = %+v", data)
	}
	if !strings.Contains(data.Preview, "改一下方向") {
		t.Fatalf("preview = %q, want the steered message", data.Preview)
	}
}

// An idle session must not emit turn/steer: the message starts its own turn.
func TestIdleUserMessageEmitsNoTurnSteerEvent(t *testing.T) {
	t.Parallel()

	sessionID := agentkit.SessionID("lark:oc_2")
	loop := &inboundRoutingLoop{}
	platform := &recordingPlatform{}
	root, err := New(Config{}, Deps{Platform: platform, Loop: loop})
	if err != nil {
		t.Fatal(err)
	}
	r := root.(*Root)
	sched := newScheduler(1, func(context.Context, agentkit.LoopRequest) error { return nil }, nil)
	defer sched.stop()
	defer sched.wait(2 * time.Second)

	event := agentkit.MessageEvent{

		Envelope: agentkit.TurnEnvelope{
			Route: rctx.SessionRoute("lark", string(sessionID)).WithInstance("lark"), Actor: agentkit.ActorRef{UserID: "ou_2"},
		},
		Message: agentkit.ModelMessage{Role: "user", Content: []agentkit.ContentPart{{Type: "text", Text: "hi"}}},
	}
	r.handleInbound(context.Background(), sched, event)

	if events := platform.steerEvents(); len(events) != 0 {
		t.Fatalf("turn/steer events = %d, want none for an idle session", len(events))
	}
}

// End-to-end within runner+loop: a second message while a turn runs must be
// steered and must reach the platform as a turn/steer event keyed to its own
// message, while the trigger message keeps its own tracking.
func TestTurnSteerEventEndToEnd(t *testing.T) {
	t.Parallel()

	sessionID := agentkit.SessionID("lark:oc_e2e")
	release := make(chan struct{})
	started := make(chan struct{}, 1)

	ag := &blockingAgent{started: started, release: release}
	loopPlugin, err := loop.New(loop.Config{}, loop.Deps{Agents: []agentkit.Agent{ag}})
	if err != nil {
		t.Fatal(err)
	}
	platform := &recordingPlatform{}
	root, err := New(Config{}, Deps{Platform: platform, Loop: loopPlugin})
	if err != nil {
		t.Fatal(err)
	}
	r := root.(*Root)
	sched := newScheduler(1, r.dispatch, nil)
	defer sched.stop()

	route := func(replyTo string) agentkit.TurnEnvelope {
		return agentkit.TurnEnvelope{Route: rctx.BuildSessionRoute(agentkit.SessionRouteInput{
			Platform: "lark", DeliveryID: sessionID, ChannelID: "oc_e2e", ReplyTo: replyTo, ScopeUserID: "ou_9",
		})}
	}
	inbound := func(replyTo, text string) agentkit.MessageEvent {
		return agentkit.MessageEvent{

			Envelope: route(replyTo).WithPlatformInstance("lark").WithUserID("ou_9").WithAgentID(ag.ID()),

			Message: agentkit.ModelMessage{Role: "user", Content: []agentkit.ContentPart{{Type: "text", Text: text}}},
		}
	}

	r.handleInbound(context.Background(), sched, inbound("om_1", "第一条"))
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("turn did not start")
	}

	r.handleInbound(context.Background(), sched, inbound("om_2", "第二条"))
	close(release)

	deadline := time.Now().Add(2 * time.Second)
	for {
		if len(platform.steerEvents()) == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("turn/steer events = %d, want 1", len(platform.steerEvents()))
		}
		time.Sleep(10 * time.Millisecond)
	}
	got := platform.steerEvents()[0]
	if replyTo := rctx.RouteReplyTo(got.Route); replyTo != "om_2" {
		t.Fatalf("steer anchor = %q, want om_2", replyTo)
	}
}

type blockingAgent struct {
	started chan struct{}
	release chan struct{}
}

func (a *blockingAgent) ID() agentkit.AgentID { return "blocker" }

func (a *blockingAgent) RunTurn(ctx context.Context, input agentkit.TurnInput) error {
	select {
	case a.started <- struct{}{}:
	default:
	}
	select {
	case <-a.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// A loop that implements SteerReporter gets the message's ack, and a terminal
// outcome lands as a second turn/steer event with the real outcome.
func TestSteerReporterReceivesTerminalOutcome(t *testing.T) {
	t.Parallel()

	sessionID := agentkit.SessionID("lark:oc_ack")
	loop := &ackLoop{
		inboundRoutingLoop: inboundRoutingLoop{busy: map[agentkit.SessionID]bool{sessionID: true}},
	}
	platform := &recordingPlatform{}
	root, err := New(Config{}, Deps{Platform: platform, Loop: loop})
	if err != nil {
		t.Fatal(err)
	}
	r := root.(*Root)
	sched := newScheduler(1, func(context.Context, agentkit.LoopRequest) error { return nil }, nil)
	defer sched.stop()
	defer sched.wait(2 * time.Second)

	event := agentkit.MessageEvent{
		Envelope: agentkit.TurnEnvelope{
			AgentID: "assistant",
			Actor:   agentkit.ActorRef{UserID: "ou_7"},
			Route: rctx.BuildSessionRoute(agentkit.SessionRouteInput{
				Platform: "lark", DeliveryID: sessionID, ChannelID: "oc_ack", ReplyTo: "om_ack", ScopeUserID: "ou_7",
			}),
		},
		Message: agentkit.ModelMessage{Role: "user", Content: []agentkit.ContentPart{{Type: "text", Text: "改方向"}}},
	}
	r.handleInbound(context.Background(), sched, event)

	// Immediate queued ack, before any terminal outcome resolves.
	if got := steerOutcomes(platform.steerEvents()); len(got) != 1 || got[0] != capsession.SteerOutcomeQueued {
		t.Fatalf("immediate outcomes = %v, want [queued]", got)
	}

	loop.resolve(capsession.SteerOutcomeInjected)

	deadline := time.Now().Add(2 * time.Second)
	for {
		if outcomes := steerOutcomes(platform.steerEvents()); len(outcomes) == 2 {
			if outcomes[1] != capsession.SteerOutcomeInjected {
				t.Fatalf("terminal outcome = %q, want injected", outcomes[1])
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("steer outcomes = %v, want a terminal injected", steerOutcomes(platform.steerEvents()))
		}
		time.Sleep(10 * time.Millisecond)
	}
	// A second resolve must not emit a third event.
	loop.resolve(capsession.SteerOutcomeNextTurn)
	time.Sleep(50 * time.Millisecond)
	if got := steerOutcomes(platform.steerEvents()); len(got) != 2 {
		t.Fatalf("outcomes after duplicate resolve = %v, want exactly 2", got)
	}
}

func steerOutcomes(events []agentkit.OutboundEvent) []string {
	var out []string
	for _, ev := range events {
		var data capsession.TurnSteerData
		if err := json.Unmarshal(ev.Data, &data); err != nil {
			continue
		}
		out = append(out, data.Outcome)
	}
	return out
}

// ackLoop captures the SteerEntry so the test can drive terminal outcomes.
type ackLoop struct {
	inboundRoutingLoop
	mu  sync.Mutex
	ack agentkit.SteerAck
}

func (l *ackLoop) SteerEntry(_ context.Context, entry agentkit.SteerEntry) error {
	l.mu.Lock()
	l.ack = entry.Ack
	l.mu.Unlock()
	return nil
}

func (l *ackLoop) resolve(outcome string) {
	l.mu.Lock()
	ack := l.ack
	l.mu.Unlock()
	if ack != nil {
		ack(outcome)
	}
}

func TestBusyInboundUploadsQueueWithoutLosingBytes(t *testing.T) {
	l := &inboundRoutingLoop{busy: map[agentkit.SessionID]bool{"cli:default": true}}
	root, err := New(Config{}, Deps{Platform: eofPlatform{}, Loop: l})
	if err != nil {
		t.Fatal(err)
	}
	r := root.(*Root)
	submitted := make(chan agentkit.LoopRequest, 1)
	sched := newScheduler(1, func(_ context.Context, req agentkit.LoopRequest) error { submitted <- req; return nil }, nil)
	defer sched.stop()
	event := agentkit.MessageEvent{Envelope: agentkit.TurnEnvelope{Route: rctx.SessionRoute("cli", "cli:default")}, Message: agentkit.ModelMessage{Role: "user", Content: []agentkit.ContentPart{{Type: "text", Text: "read file"}}}, Uploads: []agentkit.InboundUpload{{Data: []byte("payload"), OrigName: "a.txt", ContentIndex: -1}}}
	r.handleInbound(context.Background(), sched, event)
	select {
	case req := <-submitted:
		if len(req.Event.Uploads) != 1 || string(req.Event.Uploads[0].Data) != "payload" {
			t.Fatal("upload bytes lost")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("upload was steered instead of queued")
	}
	sched.wait(time.Second)
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.steers) != 0 {
		t.Fatalf("steers = %v", l.steers)
	}
}
