package runner

import (
	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/session"
)

func (r *Root) BuildInboundPromptPrefixForTest(event agentkit.MessageEvent, deliveryID agentkit.SessionID) string {
	return r.buildInboundPromptPrefix(event, deliveryID)
}

func (r *Root) FormatInboundEventForTest(event agentkit.MessageEvent, deliveryID agentkit.SessionID) agentkit.MessageEvent {
	env := session.ResolveEnvelope(event, session.DefaultRoutePolicy(session.ScopeChannel))
	if deliveryID != "" {
		env.Route = session.SessionRouteFromDelivery(event.PlatformID, deliveryID, "")
	}
	return r.formatInboundEvent(event, env)
}
