package multiplex_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/lengzhao/agentkit"
	cap "github.com/lengzhao/agentkit/cap/delivery"
	"github.com/lengzhao/agentkit/runtime/platform/common"
	"github.com/lengzhao/agentkit/runtime/platform/multiplex"
	"github.com/lengzhao/agentkit/runtime/rctx"
	"github.com/lengzhao/agentkit/testing/agenttest"
	"github.com/lengzhao/pluginkit"
)

type pluginCardPlatform struct{ common.InstanceIdentity }

func (*pluginCardPlatform) PlatformID() string { return "test" }
func (*pluginCardPlatform) Receive(context.Context) (agentkit.MessageEvent, error) {
	panic("no turn required")
}
func (*pluginCardPlatform) Send(context.Context, agentkit.OutboundEvent) error {
	panic("no assistant event required")
}
func (p *pluginCardPlatform) SendCard(ctx context.Context, route agentkit.RouteRef, card *cap.Card) (cap.MessageRef, error) {
	route, _, err := common.CardRoute(ctx, route, card, "test", p.PlatformInstance())
	return cap.MessageRef{Route: route, ID: "posted"}, err
}

type cardPlugin struct {
	Cards cap.CardSender `json:"cards"`
}

func init() {
	pluginkit.Register("test/card-platform", func(struct{}, struct{}) (agentkit.Platform, error) { return &pluginCardPlatform{}, nil })
	pluginkit.Register("test/card-plugin", func(_ struct{}, deps cardPlugin) (*cardPlugin, error) { return &deps, nil })
}
func TestCustomPluginInjectsExistingPlatformCardSender(t *testing.T) {
	graph := map[string]any{
		"one":      map[string]any{"use": "test/card-platform"},
		"two":      map[string]any{"use": "test/card-platform"},
		"platform": map[string]any{"use": "platform/multiplex", "deps": map[string]any{"platforms": []string{"one", "two"}}},
		"plugin":   map[string]any{"use": "test/card-plugin", "deps": map[string]any{"cards": "platform"}},
	}
	plugin := agenttest.Build[*cardPlugin](t, graph, "plugin")
	ref, err := plugin.Cards.SendCard(context.Background(), rctx.BuildSessionRoute(agentkit.SessionRouteInput{Platform: "test", Instance: "test1", ChannelID: "chat"}), cap.NewCard().Markdown("hello").Build())
	if err != nil || ref.ID != "posted" || ref.Route.Instance != "test1" {
		t.Fatalf("%+v %v", ref, err)
	}
	// Instance selectors accepted by the existing route normalizer keep their leaf.
	alias := agentkit.RouteRef{Platform: "test1", Kind: agentkit.RouteKindSession, Target: json.RawMessage(`{"channelId":"chat"}`)}
	ref, err = plugin.Cards.SendCard(context.Background(), alias, cap.NewCard().Markdown("hello").Build())
	if err != nil || ref.Route.Instance != "test1" {
		t.Fatalf("instance lost: %+v %v", ref, err)
	}
}
func TestCustomPluginCardUnsupportedIsExplicit(t *testing.T) {
	platform, err := multiplex.New(multiplex.Config{}, multiplex.Deps{Platforms: []agentkit.Platform{&stubPlatform{id: "cli"}}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = platform.(cap.CardSender).SendCard(context.Background(), rctx.SessionRoute("cli", "cli:session"), cap.NewCard().Markdown("card").Build())
	var de *cap.Error
	if !errors.As(err, &de) || de.Code != cap.CodeUnsupported {
		t.Fatalf("err=%v", err)
	}
}
