package session

import (
	"encoding/json"
	"strings"

	"github.com/lengzhao/agentkit"
)

// SessionRouteInput carries structured session-kind route fields.
type SessionRouteInput = agentkit.SessionRouteInput

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
func BuildSessionRoute(in SessionRouteInput) agentkit.RouteRef {
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
		return BuildSessionRoute(SessionRouteInput{
			Platform:   platform,
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
	return BuildSessionRoute(SessionRouteInput{
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
