package agentkit

import (
	"encoding/json"
	"strings"
)

// RouteKind identifies the payload schema stored in RouteRef.Target.
type RouteKind string

// RouteKindSession marks a route that returns to an IM-style delivery inbox.
const RouteKindSession RouteKind = "session"

// RouteRef identifies where outbound events should be delivered.
//
// Core layers (Runner / Loop / Agent / tools) treat RouteRef as opaque and
// only store or copy it. Platform adapters decode by Kind via runtime/session
// codecs.
//
// Today only RouteKindSession is used. Future kinds (webhook, email, …) should
// add their own typed payload in Target; do not grow flat IM fields on RouteRef.
type RouteRef struct {
	Platform string          `json:"platform,omitempty"`
	Kind     RouteKind       `json:"kind"`
	Target   json.RawMessage `json:"target,omitempty"`
}

// IsZero reports whether the route carries no platform, kind, or target.
func (r RouteRef) IsZero() bool {
	return strings.TrimSpace(r.Platform) == "" &&
		strings.TrimSpace(string(r.Kind)) == "" &&
		len(r.Target) == 0
}

// HasTarget reports whether the route carries a delivery target payload.
func (r RouteRef) HasTarget() bool {
	return len(r.Target) > 0
}

type routeRefJSON struct {
	Platform string          `json:"platform"`
	Kind     string          `json:"kind"`
	Target   json.RawMessage `json:"target,omitempty"`
	Session  json.RawMessage `json:"session,omitempty"`
	Data     json.RawMessage `json:"data,omitempty"`
	// legacy flat session fields (pre-nested SessionRoute)
	SessionID string `json:"sessionId,omitempty"`
	ChannelID string `json:"channelId,omitempty"`
	ThreadID  string `json:"threadId,omitempty"`
	MessageID string `json:"messageId,omitempty"`
	UserID    string `json:"userId,omitempty"`
}

// SessionRouteTarget is the delivery payload for RouteKindSession.
//
// DeliveryID is the stable return path (active-session / delivery key). When
// empty it is derived from Platform + ChannelID + ThreadID + ScopeUserID.
//
// ReplyTo is an ephemeral inbound message anchor for threaded replies during
// this turn; it is not part of the stable delivery key.
//
// ScopeUserID is the :u: segment in delivery ids (routing scope), not the
// speaking user — see TurnEnvelope.Actor for actor identity.
type SessionRouteTarget struct {
	DeliveryID  SessionID `json:"deliveryId,omitempty"`
	ChannelID   string    `json:"channelId,omitempty"`
	ThreadID    string    `json:"threadId,omitempty"`
	ScopeUserID string    `json:"scopeUserId,omitempty"`
	ReplyTo     string    `json:"replyTo,omitempty"`
}

// HasTarget reports whether the payload carries a delivery id or channel.
func (t SessionRouteTarget) HasTarget() bool {
	return strings.TrimSpace(string(t.DeliveryID)) != "" || strings.TrimSpace(t.ChannelID) != ""
}

// UnmarshalJSON accepts target payloads and legacy session/data/flat encodings.
func (r *RouteRef) UnmarshalJSON(data []byte) error {
	var raw routeRefJSON
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	r.Platform = raw.Platform
	r.Kind = RouteKind(raw.Kind)
	r.Target = raw.Target

	if len(r.Target) > 0 {
		return nil
	}
	if len(raw.Session) > 0 {
		r.Target = append(json.RawMessage(nil), raw.Session...)
		return nil
	}
	if len(raw.Data) > 0 {
		r.Target = append(json.RawMessage(nil), raw.Data...)
		return nil
	}

	legacy := SessionRouteTarget{
		DeliveryID:  SessionID(strings.TrimSpace(raw.SessionID)),
		ChannelID:   strings.TrimSpace(raw.ChannelID),
		ThreadID:    strings.TrimSpace(raw.ThreadID),
		ReplyTo:     strings.TrimSpace(raw.MessageID),
		ScopeUserID: strings.TrimSpace(raw.UserID),
	}
	if legacy.DeliveryID == "" && legacy.ChannelID == "" {
		return nil
	}
	target, err := json.Marshal(legacy)
	if err != nil {
		return err
	}
	r.Target = target
	return nil
}

// MarshalJSON writes the stable wire form: platform, kind, target.
func (r RouteRef) MarshalJSON() ([]byte, error) {
	type wire struct {
		Platform string          `json:"platform,omitempty"`
		Kind     RouteKind       `json:"kind"`
		Target   json.RawMessage `json:"target,omitempty"`
	}
	return json.Marshal(wire{
		Platform: r.Platform,
		Kind:     r.Kind,
		Target:   r.Target,
	})
}

// ActorRef identifies who spoke on an inbound turn.
type ActorRef struct {
	UserID string `json:"userId,omitempty"`
	Name   string `json:"name,omitempty"`
	Email  string `json:"email,omitempty"`
}

// TurnEnvelope is the normalized routing context for one turn.
//
//   - Route: where outbound replies go (default: captured at inbound)
//   - Conversation: history file and Loop lock key
//   - Workspace: tenant directory for fs, shell, local MCP, skills, memory
//   - AgentID: agent executing this turn
//   - Actor: end-user identity for audit, inject, and permission
//   - Metadata: platform and plugin extensions
type TurnEnvelope struct {
	Route        RouteRef       `json:"route"`
	Conversation string         `json:"conversation"`
	Workspace    string         `json:"workspace"`
	AgentID      AgentID        `json:"agentId,omitempty"`
	Actor        ActorRef       `json:"actor"`
	Metadata     map[string]any `json:"metadata,omitempty"`
}

// WithRoute returns a copy with Route replaced.
func (e TurnEnvelope) WithRoute(route RouteRef) TurnEnvelope {
	e.Route = route
	return e
}

// WithConversation returns a copy with Conversation replaced.
func (e TurnEnvelope) WithConversation(conversation string) TurnEnvelope {
	e.Conversation = conversation
	return e
}

// WithWorkspace returns a copy with Workspace replaced.
func (e TurnEnvelope) WithWorkspace(workspace string) TurnEnvelope {
	e.Workspace = workspace
	return e
}

// WithAgentID returns a copy with AgentID replaced.
func (e TurnEnvelope) WithAgentID(agentID AgentID) TurnEnvelope {
	e.AgentID = agentID
	return e
}

// WithMetadata returns a copy with Metadata replaced.
func (e TurnEnvelope) WithMetadata(metadata map[string]any) TurnEnvelope {
	e.Metadata = metadata
	return e
}

// SessionRoute builds a session-kind route from a delivery id string.
//
// Deprecated: prefer runtime/session.SessionRouteFromDelivery or BuildSessionRoute
// so ReplyTo and channel/thread fields are preserved when needed.
func SessionRoute(platform, deliveryID string) RouteRef {
	platform = strings.TrimSpace(platform)
	deliveryID = strings.TrimSpace(deliveryID)
	target, _ := json.Marshal(SessionRouteTarget{DeliveryID: SessionID(deliveryID)})
	return RouteRef{
		Platform: platform,
		Kind:     RouteKindSession,
		Target:   target,
	}
}
