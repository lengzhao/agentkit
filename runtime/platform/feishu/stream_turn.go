package feishu

import (
	"strings"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/rctx"
)

// streamKeyForOutbound returns the in-memory stream key for an outbound event:
// the loop-stamped TurnID when present, else the legacy delivery+replyTo key.
// It also captures the event's route into stream state so async paths
// (heartbeat, debounced flush, turn/end finalize) can resolve the IM target
// without re-reading the event.
func (p *Platform) streamKeyForOutbound(event agentkit.OutboundEvent) agentkit.SessionID {
	key := outboundStreamKey(event)
	if !event.Route.IsZero() {
		st := p.streamState(key)
		st.lock()
		if !st.hasRoute {
			st.route = event.Route
			st.hasRoute = true
		}
		st.unlock()
	}
	return key
}

// replyContextFromRoute resolves the IM reply target from a route ref.
func (p *Platform) replyContextFromRoute(route agentkit.RouteRef) (replyContext, bool) {
	delivery, ok := rctx.RouteSessionID(route)
	if !ok || delivery == "" {
		return replyContext{}, false
	}
	rc, ok := p.deliveryForSend(delivery)
	if !ok {
		return replyContext{}, false
	}
	if replyTo := strings.TrimSpace(rctx.RouteReplyTo(route)); replyTo != "" {
		rc.messageID = replyTo
	}
	return rc, true
}

// replyContextForStream resolves the IM target for a stream: prefer the route
// captured in stream state (turn-keyed streams), else decode the legacy
// delivery+replyTo stream key.
func (p *Platform) replyContextForStream(streamKey agentkit.SessionID) (replyContext, bool) {
	st := p.streamState(streamKey)
	st.lock()
	route, hasRoute := st.route, st.hasRoute
	st.unlock()
	if hasRoute {
		return p.replyContextFromRoute(route)
	}
	// Legacy path: stream key encodes delivery (+ replyTo).
	delivery := deliveryFromStreamKey(streamKey)
	rc, ok := p.deliveryForSend(delivery)
	if !ok {
		return replyContext{}, false
	}
	if replyTo := replyToFromStreamKey(streamKey); replyTo != "" {
		rc.messageID = replyTo
	}
	return rc, true
}

// deliveryForStream returns the delivery id for a stream (for plain-text
// fallback), preferring the captured route.
func (p *Platform) deliveryForStream(streamKey agentkit.SessionID) agentkit.SessionID {
	st := p.streamState(streamKey)
	st.lock()
	route, hasRoute := st.route, st.hasRoute
	st.unlock()
	if hasRoute {
		if delivery, ok := rctx.RouteSessionID(route); ok {
			return delivery
		}
	}
	return deliveryFromStreamKey(streamKey)
}
