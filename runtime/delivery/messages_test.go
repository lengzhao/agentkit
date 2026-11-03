package delivery

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/lengzhao/agentkit"
	cap "github.com/lengzhao/agentkit/cap/delivery"
	"github.com/lengzhao/agentkit/runtime/rctx"
)

type messageLeaf struct {
	id      string
	calls   int
	receipt cap.Receipt
	err     error
}

func (p *messageLeaf) PlatformID() string { return "test" }
func (p *messageLeaf) Receive(context.Context) (agentkit.MessageEvent, error) {
	panic("Receive must not be needed")
}
func (p *messageLeaf) Send(context.Context, agentkit.OutboundEvent) error {
	panic("legacy Send must not be used")
}
func (p *messageLeaf) Publish(_ context.Context, r cap.PublishRequest) (cap.Receipt, error) {
	p.calls++
	if p.err != nil {
		return p.receipt, p.err
	}
	return cap.Receipt{Route: r.Route, Messages: []cap.MessageRef{{Route: r.Route, ID: "message"}}, Outcome: cap.OutcomeConfirmed}, nil
}
func (p *messageLeaf) ReadMessage(_ context.Context, r cap.ReadRequest) (cap.Message, error) {
	return cap.Message{Ref: r.Message, Content: []cap.Part{{Type: "text", Text: "restored"}}}, nil
}
func testRoute(instance string) agentkit.RouteRef {
	return rctx.BuildSessionRoute(agentkit.SessionRouteInput{Platform: "test", Instance: instance, ChannelID: "channel"})
}
func TestMessagesNoTurnAndPersistentReference(t *testing.T) {
	a, b := &messageLeaf{}, &messageLeaf{}
	svc, err := NewMessages(MessagesConfig{Instances: []string{"a", "b"}}, MessagesDeps{Platforms: []agentkit.Platform{a, b}})
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := svc.Publish(context.Background(), cap.PublishRequest{Route: testRoute("b"), Content: []cap.Part{{Type: "text", Text: "hi"}}})
	if err != nil || a.calls != 0 || b.calls != 1 || receipt.Route.Instance != "b" {
		t.Fatalf("receipt=%+v err=%v calls=%d/%d", receipt, err, a.calls, b.calls)
	}
	raw, _ := json.Marshal(receipt.Messages[0])
	var ref cap.MessageRef
	if err = json.Unmarshal(raw, &ref); err != nil {
		t.Fatal(err)
	}
	restored, _ := NewMessages(MessagesConfig{Instances: []string{"a", "b"}}, MessagesDeps{Platforms: []agentkit.Platform{&messageLeaf{}, &messageLeaf{}}})
	msg, err := restored.ReadMessage(context.Background(), cap.ReadRequest{Message: ref})
	if err != nil || msg.Content[0].Text != "restored" {
		t.Fatalf("%+v %v", msg, err)
	}
	_, err = svc.CreateTopic(context.Background(), cap.PublishRequest{Route: testRoute("b"), Content: []cap.Part{{Type: "text", Text: "hi"}}})
	var de *cap.Error
	if !errors.As(err, &de) || de.Code != cap.CodeUnsupported {
		t.Fatalf("expected unsupported topic: %v", err)
	}
}
func TestMessagesRejectWrongInstanceAndKeepPartialUnknown(t *testing.T) {
	partial := cap.Receipt{Route: testRoute("a"), Messages: []cap.MessageRef{{Route: testRoute("a"), ID: "first"}}, Outcome: cap.OutcomeUnknown}
	leaf := &messageLeaf{receipt: partial, err: &cap.Error{Code: cap.CodeTransport, Outcome: cap.OutcomeUnknown}}
	svc, _ := NewMessages(MessagesConfig{Instances: []string{"a"}}, MessagesDeps{Platforms: []agentkit.Platform{leaf}})
	_, err := svc.ReadMessage(context.Background(), cap.ReadRequest{Message: cap.MessageRef{Route: testRoute("other"), ID: "first"}})
	if err == nil {
		t.Fatal("wrong instance accepted")
	}
	got, err := svc.Publish(context.Background(), cap.PublishRequest{Route: testRoute("a"), Content: []cap.Part{{Type: "text", Text: "hi"}}})
	if err == nil || len(got.Messages) != 1 || got.Outcome != cap.OutcomeUnknown || leaf.calls != 1 {
		t.Fatalf("%+v %v calls=%d", got, err, leaf.calls)
	}
}
func TestMessagesAdmissionAndAmbiguousInstances(t *testing.T) {
	if _, err := NewMessages(MessagesConfig{}, MessagesDeps{Platforms: []agentkit.Platform{&messageLeaf{}, &messageLeaf{}}}); err == nil {
		t.Fatal("duplicate identity accepted")
	}
	leaf := &messageLeaf{}
	svc, _ := NewMessages(MessagesConfig{Instances: []string{"a"}}, MessagesDeps{Platforms: []agentkit.Platform{leaf}, Admission: denyAdmission{}})
	_, err := svc.Publish(context.Background(), cap.PublishRequest{Route: testRoute("a"), Content: []cap.Part{{Type: "text", Text: "hi"}}})
	if err == nil || leaf.calls != 0 {
		t.Fatalf("admission bypassed: %v", err)
	}
}

type denyAdmission struct{}

func (denyAdmission) AuthorizeDelivery(context.Context, cap.Call) error { return errors.New("denied") }

type captureAdmission struct{ call cap.Call }

func (a *captureAdmission) AuthorizeDelivery(_ context.Context, c cap.Call) error {
	a.call = c
	return nil
}
func TestMessagesReadCarriesCallerAndOperation(t *testing.T) {
	policy := &captureAdmission{}
	svc, _ := NewMessages(MessagesConfig{Instances: []string{"a"}}, MessagesDeps{Platforms: []agentkit.Platform{&messageLeaf{}}, Admission: policy})
	_, err := svc.ReadMessage(context.Background(), cap.ReadRequest{Message: cap.MessageRef{Route: testRoute("a"), ID: "m"}, Caller: "restore", OperationID: "lookup"})
	if err != nil || policy.call.Caller != "restore" || policy.call.OperationID != "lookup" || policy.call.Operation != "read" {
		t.Fatalf("%+v %v", policy.call, err)
	}
}
