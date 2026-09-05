package session

import (
	"encoding/json"
	"strings"

	"github.com/lengzhao/agentkit"
)

type sessionRouteData struct {
	ID string `json:"id"`
}

// SessionRouteInput carries structured session-kind route fields.
type SessionRouteInput struct {
	Platform    string
	DeliveryID  agentkit.SessionID
	ChannelID   string
	ThreadID    string
	ReplyTo     string
	ScopeUserID string
}

// RouteTarget is the decoded delivery target for a session-kind route.
type RouteTarget struct {
	DeliveryID  agentkit.SessionID
	ChannelID   string
	ThreadID    string
	ReplyTo     string
	ScopeUserID string
}

func encodeSessionRouteTarget(target agentkit.SessionRouteTarget) (json.RawMessage, error) {
	return json.Marshal(target)
}

func sessionRouteFromTarget(platform string, target agentkit.SessionRouteTarget) agentkit.RouteRef {
	raw, err := encodeSessionRouteTarget(target)
	if err != nil {
		return agentkit.RouteRef{}
	}
	return agentkit.RouteRef{
		Platform: strings.TrimSpace(platform),
		Kind:     agentkit.RouteKindSession,
		Target:   raw,
	}
}

// BuildSessionRoute builds a session-kind RouteRef from structured fields.
// When DeliveryID is empty, it is derived from platform/channel/thread/scope user.
func BuildSessionRoute(in SessionRouteInput) agentkit.RouteRef {
	platform := strings.TrimSpace(in.Platform)
	channel := strings.TrimSpace(in.ChannelID)
	thread := strings.TrimSpace(in.ThreadID)
	scopeUser := strings.TrimSpace(in.ScopeUserID)
	replyTo := strings.TrimSpace(in.ReplyTo)
	deliveryID := strings.TrimSpace(string(in.DeliveryID))
	if deliveryID == "" && platform != "" && channel != "" {
		deliveryID = string(BuildDeliverySessionID(platform, channel, thread, scopeUser))
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
		return sessionRouteFromTarget(platform, agentkit.SessionRouteTarget{
			DeliveryID: delivery,
			ReplyTo:    strings.TrimSpace(replyTo),
		})
	}
	return BuildSessionRoute(SessionRouteInput{
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
		id := BuildDeliverySessionID(platform, channel, target.ThreadID, target.ScopeUserID)
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
