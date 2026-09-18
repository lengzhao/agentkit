package rctx

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/lengzhao/agentkit"
)

type sessionRouteData struct {
	ID string `json:"id"`
}

// RouteTarget is the decoded delivery target for a session-kind route.
type RouteTarget struct {
	DeliveryID  agentkit.SessionID
	ChannelID   string
	ThreadID    string
	ReplyTo     string
	ScopeUserID string
}

// BuildSessionRoute builds a session-kind RouteRef from structured fields.
func BuildSessionRoute(in agentkit.SessionRouteInput) agentkit.RouteRef {
	platform := strings.TrimSpace(in.Platform)
	channel := strings.TrimSpace(in.ChannelID)
	thread := strings.TrimSpace(in.ThreadID)
	scopeUser := strings.TrimSpace(in.ScopeUserID)
	replyTo := strings.TrimSpace(in.ReplyTo)
	deliveryID := strings.TrimSpace(string(in.DeliveryID))
	if deliveryID == "" && platform != "" && channel != "" {
		deliveryID = string(buildDeliverySessionID(platform, channel, thread, scopeUser))
	}
	return sessionRouteFromTarget(platform, agentkit.SessionRouteTarget{
		DeliveryID:  agentkit.SessionID(deliveryID),
		ChannelID:   channel,
		ThreadID:    thread,
		ScopeUserID: scopeUser,
		ReplyTo:     replyTo,
	})
}

// SessionRouteFromDelivery builds a session route from a delivery SessionID.
func SessionRouteFromDelivery(platform string, delivery agentkit.SessionID, replyTo string) agentkit.RouteRef {
	delivery = agentkit.SessionID(strings.TrimSpace(string(delivery)))
	if delivery == "" {
		return agentkit.RouteRef{}
	}
	parts := ParseDelivery(delivery, "")
	if !parts.Routable {
		return BuildSessionRoute(agentkit.SessionRouteInput{
			Platform:   platform,
			DeliveryID: delivery,
			ReplyTo:    strings.TrimSpace(replyTo),
		})
	}
	return BuildSessionRoute(agentkit.SessionRouteInput{
		Platform:    platform,
		DeliveryID:  delivery,
		ChannelID:   parts.Channel,
		ThreadID:    parts.Thread,
		ScopeUserID: parts.User,
		ReplyTo:     replyTo,
	})
}

// DecodeSessionRoute decodes a session-kind RouteRef into SessionRouteTarget.
func DecodeSessionRoute(route agentkit.RouteRef) (agentkit.SessionRouteTarget, bool) {
	if route.Kind != "" && route.Kind != agentkit.RouteKindSession {
		return agentkit.SessionRouteTarget{}, false
	}
	if len(route.Target) == 0 {
		return agentkit.SessionRouteTarget{}, false
	}

	var target agentkit.SessionRouteTarget
	if err := json.Unmarshal(route.Target, &target); err == nil && target.HasTarget() {
		return target, true
	}

	var id string
	if err := json.Unmarshal(route.Target, &id); err == nil {
		id = strings.TrimSpace(id)
		if id != "" {
			return agentkit.SessionRouteTarget{DeliveryID: agentkit.SessionID(id)}, true
		}
	}

	var data sessionRouteData
	if err := json.Unmarshal(route.Target, &data); err != nil {
		return agentkit.SessionRouteTarget{}, false
	}
	id = strings.TrimSpace(data.ID)
	if id == "" {
		return agentkit.SessionRouteTarget{}, false
	}
	return agentkit.SessionRouteTarget{DeliveryID: agentkit.SessionID(id)}, true
}

// RouteSessionID decodes a session-kind RouteRef into a delivery SessionID.
func RouteSessionID(route agentkit.RouteRef) (agentkit.SessionID, bool) {
	target, ok := DecodeSessionRoute(route)
	if !ok {
		return "", false
	}
	if id := strings.TrimSpace(string(target.DeliveryID)); id != "" {
		return agentkit.SessionID(id), true
	}
	platform := strings.TrimSpace(route.Platform)
	channel := strings.TrimSpace(target.ChannelID)
	if platform != "" && channel != "" {
		id := buildDeliverySessionID(platform, channel, target.ThreadID, target.ScopeUserID)
		if id != "" {
			return id, true
		}
	}
	return "", false
}

// RouteTargetFromRoute decodes structured delivery fields from a session route.
func RouteTargetFromRoute(route agentkit.RouteRef) (RouteTarget, bool) {
	id, ok := RouteSessionID(route)
	if !ok {
		return RouteTarget{}, false
	}
	target, ok := DecodeSessionRoute(route)
	if !ok {
		return RouteTarget{}, false
	}
	out := RouteTarget{
		DeliveryID:  id,
		ChannelID:   strings.TrimSpace(target.ChannelID),
		ThreadID:    strings.TrimSpace(target.ThreadID),
		ReplyTo:     strings.TrimSpace(target.ReplyTo),
		ScopeUserID: strings.TrimSpace(target.ScopeUserID),
	}
	if out.ChannelID == "" {
		parts := ParseDelivery(id, out.ScopeUserID)
		if parts.Routable {
			out.ChannelID = parts.Channel
			out.ThreadID = parts.Thread
			if out.ScopeUserID == "" {
				out.ScopeUserID = parts.User
			}
		}
	}
	return out, true
}

// RouteReplyTo returns the ephemeral reply anchor for this turn, if any.
func RouteReplyTo(route agentkit.RouteRef) string {
	target, ok := DecodeSessionRoute(route)
	if !ok {
		return ""
	}
	return strings.TrimSpace(target.ReplyTo)
}

// OutboundRouteID returns the platform routing target for an outbound event.
func OutboundRouteID(event agentkit.OutboundEvent) agentkit.SessionID {
	if id, ok := RouteSessionID(event.Route); ok && id != "" {
		return id
	}
	return ""
}

func sessionRouteFromTarget(platform string, target agentkit.SessionRouteTarget) agentkit.RouteRef {
	raw, err := json.Marshal(target)
	if err != nil {
		return agentkit.RouteRef{}
	}
	return agentkit.RouteRef{
		Platform: strings.TrimSpace(platform),
		Kind:     agentkit.RouteKindSession,
		Target:   raw,
	}
}

// SessionRoute builds a minimal session-kind route from a delivery id string.
func SessionRoute(platform, deliveryID string) agentkit.RouteRef {
	return BuildSessionRoute(agentkit.SessionRouteInput{
		Platform:   strings.TrimSpace(platform),
		DeliveryID: agentkit.SessionID(strings.TrimSpace(deliveryID)),
	})
}

func buildDeliverySessionID(platform, channel, thread, user string) agentkit.SessionID {
	platform = strings.TrimSpace(platform)
	channel = strings.TrimSpace(channel)
	if platform == "" || channel == "" {
		return ""
	}
	id := platform + ":" + channel
	if thread = strings.TrimSpace(thread); thread != "" {
		id += ":t:" + thread
	}
	if user = strings.TrimSpace(user); user != "" {
		id += ":u:" + user
	}
	return agentkit.SessionID(id)
}

// DeliveryParts holds parsed segments of a platform delivery SessionID.
type DeliveryParts struct {
	Platform string
	Channel  string
	Thread   string
	User     string
	Routable bool
}

// opaqueDeliveryPlatforms lists platforms whose two-segment ids (platform:segment)
// should not be treated as IM-style routable deliveries. These are runtime
// conventions for headless/CLI transports, not a root-package contract.
var opaqueDeliveryPlatforms = map[string]bool{
	"cli":    true,
	"sub":    true,
	"jsonl":  true,
	"worker": true,
	"timer":  true,
	"cron":   true,
}

// ParseDelivery splits a delivery SessionID into routing segments.
func ParseDelivery(id agentkit.SessionID, fallbackUser string) DeliveryParts {
	return parseDeliveryParts(string(id), fallbackUser)
}

// BuildDeliverySessionID is the canonical finest-grain id platforms should emit.
func BuildDeliverySessionID(platform, channel, thread, user string) agentkit.SessionID {
	platform = strings.TrimSpace(platform)
	channel = strings.TrimSpace(channel)
	if platform == "" || channel == "" {
		return ""
	}
	id := platform + ":" + channel
	if thread = strings.TrimSpace(thread); thread != "" {
		id += ":t:" + thread
	}
	if user = strings.TrimSpace(user); user != "" {
		id += ":u:" + user
	}
	return agentkit.SessionID(id)
}

// ApplyScope derives the effective session id used for Loop locking, history,
// and permission pending.
func ApplyScope(delivery agentkit.SessionID, scope agentkit.SessionScope, userID string) agentkit.SessionID {
	id := strings.TrimSpace(string(delivery))
	if id == "" {
		return delivery
	}
	parts := parseDeliveryParts(id, userID)
	if !parts.Routable {
		return delivery
	}
	return parts.effective(ParseScope(string(scope)))
}

// DeliveryWithUser returns a delivery SessionID with the :u: segment set or replaced.
func DeliveryWithUser(delivery agentkit.SessionID, userID string) agentkit.SessionID {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return delivery
	}
	parts := ParseDelivery(delivery, "")
	if !parts.Routable {
		return delivery
	}
	parts.User = userID
	return BuildDeliverySessionID(parts.Platform, parts.Channel, parts.Thread, parts.User)
}

func parseDeliveryParts(id, fallbackUser string) DeliveryParts {
	segments := strings.Split(id, ":")
	if len(segments) < 2 {
		return DeliveryParts{Routable: false}
	}

	p := DeliveryParts{
		Platform: segments[0],
		Channel:  segments[1],
		Routable: true,
	}

	if p.Platform == "schedule" {
		p.Routable = false
		return p
	}

	if opaqueDeliveryPlatforms[p.Platform] && len(segments) == 2 {
		p.Routable = false
		return p
	}

	for i := 2; i < len(segments); {
		switch segments[i] {
		case "t":
			if i+1 >= len(segments) {
				i++
				continue
			}
			p.Thread = segments[i+1]
			i += 2
		case "u":
			if i+1 >= len(segments) {
				i++
				continue
			}
			p.User = segments[i+1]
			i += 2
		default:
			i++
		}
	}

	if p.User == "" {
		p.User = strings.TrimSpace(fallbackUser)
	}
	return p
}

func (p DeliveryParts) effective(scope agentkit.SessionScope) agentkit.SessionID {
	base := p.Platform + ":" + p.Channel
	switch scope {
	case agentkit.SessionScopeChannel:
		return agentkit.SessionID(base)
	case agentkit.SessionScopeUser:
		if p.User == "" {
			return agentkit.SessionID(base)
		}
		return agentkit.SessionID(base + ":u:" + p.User)
	default:
		if p.Thread == "" {
			return agentkit.SessionID(base)
		}
		return agentkit.SessionID(base + ":t:" + p.Thread)
	}
}

// DeliveryRouteFromContext returns the platform delivery target from the turn envelope.
func DeliveryRouteFromContext(ctx context.Context) agentkit.SessionID {
	if id, ok := RouteSessionID(EnvelopeFromContext(ctx).Route); ok && id != "" {
		return id
	}
	return ""
}

// RouteRefFromContext returns the outbound route from the turn envelope.
func RouteRefFromContext(ctx context.Context) agentkit.RouteRef {
	return EnvelopeFromContext(ctx).Route
}

// ContextWithDeliveryRoute attaches a minimal session-kind delivery route to ctx.
func ContextWithDeliveryRoute(ctx context.Context, platform string, delivery agentkit.SessionID) context.Context {
	env := EnvelopeFromContext(ctx)
	env.Route = SessionRouteFromDelivery(platform, delivery, "")
	return ApplyEnvelopeToContext(ctx, env)
}

// InboundDeliveryID returns the platform delivery target from an inbound message.
func InboundDeliveryID(event agentkit.MessageEvent) agentkit.SessionID {
	return DeliveryFromEnvelope(event.Envelope)
}

// DeliveryFromEnvelope returns the outbound delivery id from a turn envelope.
func DeliveryFromEnvelope(env agentkit.TurnEnvelope) agentkit.SessionID {
	id, _ := RouteSessionID(env.Route)
	return id
}
