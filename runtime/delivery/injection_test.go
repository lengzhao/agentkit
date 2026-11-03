package delivery_test

import (
	"context"
	"testing"

	"github.com/lengzhao/agentkit"
	cap "github.com/lengzhao/agentkit/cap/delivery"
	_ "github.com/lengzhao/agentkit/runtime/delivery"
	"github.com/lengzhao/agentkit/runtime/platform/common"
	_ "github.com/lengzhao/agentkit/runtime/platform/multiplex"
	"github.com/lengzhao/agentkit/runtime/rctx"
	"github.com/lengzhao/agentkit/testing/agenttest"
	"github.com/lengzhao/pluginkit"
)

type injectedLeaf struct{ common.InstanceIdentity }

func (*injectedLeaf) PlatformID() string                                     { return "test" }
func (*injectedLeaf) Receive(context.Context) (agentkit.MessageEvent, error) { panic("not a turn") }
func (*injectedLeaf) Send(context.Context, agentkit.OutboundEvent) error {
	panic("not an assistant event")
}
func (*injectedLeaf) Publish(_ context.Context, r cap.PublishRequest) (cap.Receipt, error) {
	return cap.Receipt{Route: r.Route, Messages: []cap.MessageRef{{Route: r.Route, ID: "durable"}}, Outcome: cap.OutcomeConfirmed}, nil
}

type injectedConsumer struct {
	Publisher cap.Publisher     `json:"publisher"`
	Platform  agentkit.Platform `json:"platform"`
}

func init() {
	pluginkit.Register("test/delivery-leaf", func(struct{}, struct{}) (agentkit.Platform, error) { return &injectedLeaf{}, nil })
	pluginkit.Register("test/delivery-consumer", func(_ struct{}, deps injectedConsumer) (*injectedConsumer, error) { return &deps, nil })
}
func TestMessagesRealDependencyInjectionWithoutTurn(t *testing.T) {
	graph := map[string]any{
		"work":     map[string]any{"use": "test/delivery-leaf"},
		"home":     map[string]any{"use": "test/delivery-leaf"},
		"messages": map[string]any{"use": "delivery/messages", "config": map[string]any{"instances": []string{"work", "home"}}, "deps": map[string]any{"platforms": []string{"work", "home"}}},
		"platform": map[string]any{"use": "platform/multiplex", "config": map[string]any{"instances": []string{"work", "home"}}, "deps": map[string]any{"platforms": []string{"work", "home"}}},
		"consumer": map[string]any{"use": "test/delivery-consumer", "deps": map[string]any{"publisher": "messages", "platform": "platform"}},
	}
	consumer := agenttest.Build[*injectedConsumer](t, graph, "consumer")
	receipt, err := consumer.Publisher.Publish(context.Background(), cap.PublishRequest{Route: rctx.BuildSessionRoute(agentkit.SessionRouteInput{Platform: "test", Instance: "home", ChannelID: "channel"}), Content: []cap.Part{{Type: "text", Text: "background"}}})
	if err != nil || receipt.Route.Instance != "home" || len(receipt.Messages) != 1 {
		t.Fatalf("%+v %v", receipt, err)
	}
}
