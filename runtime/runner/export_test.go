package runner

import (
	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/rctx"
	sessstore "github.com/lengzhao/agentkit/runtime/session/sessstore"
)

func (r *Root) BuildInboundPromptPrefixForTest(event agentkit.MessageEvent, deliveryID agentkit.SessionID) string {
	return r.buildInboundPromptPrefix(event, deliveryID)
}

func (r *Root) FormatInboundEventForTest(event agentkit.MessageEvent, deliveryID agentkit.SessionID) agentkit.MessageEvent {
	env := rctx.ResolveEnvelope(event, rctx.DefaultRoutePolicy(sessstore.ScopeChannel))
	if deliveryID != "" {
		env.Route = rctx.SessionRouteFromDelivery(event.PlatformID, deliveryID, "")
	}
	return r.formatInboundEvent(event, env)
}
