package send_test

import (
	"context"
	"encoding/json"
	"testing"

	cap "github.com/lengzhao/agentkit/cap/delivery"
	"github.com/lengzhao/agentkit/plugins/tool/send"
	"github.com/lengzhao/agentkit/runtime/rctx"
)

type receiptPublisher struct {
	calls   int
	request cap.PublishRequest
}

func (p *receiptPublisher) Publish(_ context.Context, req cap.PublishRequest) (cap.Receipt, error) {
	p.calls++
	p.request = req
	return cap.Receipt{Route: req.Route, Messages: []cap.MessageRef{{Route: req.Route, ID: "persist-me"}}, Outcome: cap.OutcomeConfirmed}, nil
}
func TestSendReceiptMode(t *testing.T) {
	publisher := &receiptPublisher{}
	legacy := &recordingPlatform{}
	tool, err := send.NewSend(send.SendConfig{}, send.SendDeps{Sender: legacy, Delivery: mustAssistant(t), Messages: publisher})
	if err != nil {
		t.Fatal(err)
	}
	route := rctx.SessionRoute("slack", "slack:C001").WithInstance("work")
	raw, _ := json.Marshal(map[string]any{"text": "hello", "receipt": true, "operationId": "op1", "route": route})
	out, err := tool.Call(context.Background(), raw)
	if err != nil {
		t.Fatal(err)
	}
	var result send.SendOutput
	if err = json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatal(err)
	}
	if !result.Sent || result.Receipt == nil || result.Receipt.Messages[0].ID != "persist-me" || publisher.calls != 1 || len(legacy.sent) != 0 || publisher.request.OperationID != "op1" {
		t.Fatalf("%+v publisher=%+v", result, publisher)
	}
}
func TestSendReceiptRequiresCapability(t *testing.T) {
	legacy := &recordingPlatform{}
	tool, err := send.NewSend(send.SendConfig{}, send.SendDeps{Sender: legacy, Delivery: mustAssistant(t)})
	if err != nil {
		t.Fatal(err)
	}
	out, err := tool.Call(context.Background(), []byte(`{"text":"hello","receipt":true}`))
	var result send.SendOutput
	json.Unmarshal([]byte(out), &result)
	if err != nil || result.ErrorCode != cap.CodeUnsupported || result.Sent || len(legacy.sent) != 0 {
		t.Fatalf("out=%s err=%v legacy=%v", out, err, legacy.sent)
	}
}

func TestSendReceiptRouteSchemaIsObject(t *testing.T) {
	tool, err := send.NewSend(send.SendConfig{}, send.SendDeps{Sender: &recordingPlatform{}, Delivery: mustAssistant(t)})
	if err != nil {
		t.Fatal(err)
	}
	route := tool.InputSchema().Properties["route"]
	target := route.Properties["target"]
	if target.Type != "object" || target.Properties["channelId"].Type != "string" {
		raw, _ := json.Marshal(route)
		t.Fatalf("route target schema must describe an object: %s", raw)
	}
}

type unknownPublisher struct{}

func (unknownPublisher) Publish(_ context.Context, req cap.PublishRequest) (cap.Receipt, error) {
	failed := 1
	return cap.Receipt{Route: req.Route, Messages: []cap.MessageRef{{Route: req.Route, ID: "confirmed-first"}}, Outcome: cap.OutcomeUnknown, FailedPart: &failed}, &cap.Error{Code: cap.CodeTransport, Outcome: cap.OutcomeUnknown}
}
func TestSendReceiptPreservesPartialUnknownResult(t *testing.T) {
	legacy := &recordingPlatform{}
	tool, err := send.NewSend(send.SendConfig{}, send.SendDeps{Sender: legacy, Delivery: mustAssistant(t), Messages: unknownPublisher{}})
	if err != nil {
		t.Fatal(err)
	}
	out, err := tool.Call(context.Background(), []byte(`{"text":"hello","receipt":true,"route":{"platform":"slack","instance":"work","kind":"session","target":{"channelId":"C123"}}}`))
	var result send.SendOutput
	json.Unmarshal([]byte(out), &result)
	if err != nil || result.Sent || result.ErrorCode != cap.CodeTransport || result.Receipt == nil || len(result.Receipt.Messages) != 1 || result.Receipt.Outcome != cap.OutcomeUnknown || len(legacy.sent) != 0 {
		t.Fatalf("out=%s err=%v", out, err)
	}
}
