package rctx

import (
	"context"
	"strings"

	"github.com/lengzhao/agentkit"
)

// BindKeys holds a turn's resolution keys for every dynamic-binding level,
// computed in one place so no caller hand-rolls a delivery id. The global level
// is workspace-scoped and needs no key, so it is not a field here.
//
// Granularity, coarse to fine: Channel < Thread|User < Session. Thread and User
// are mutually exclusive siblings (a delivery id carries :t: or :u:, not both).
// A level is empty when the route carries no such segment. Session is the
// resolved conversation history key, which may already point at a /new child.
type BindKeys struct {
	Channel agentkit.SessionID
	Thread  agentkit.SessionID
	User    agentkit.SessionID
	Session agentkit.SessionID
}

// BindKeysForRoute derives every route-scoped key under policy from the route's
// structured target (ChannelID / ThreadID / ScopeUserID), which BuildSessionRoute
// now fills for every construction path. session is the already-resolved
// conversation key (inbound: the turn conversation; slash: SessionIDFromContext,
// optionally upgraded to a /new child with ResolveSessionKey).
//
// Levels reuse the same granularity as ApplyScope, so a bind key equals the
// session id the runner would lock on for that scope. userID is the actor id
// (rctx.UserIDFromContext) used when neither the delivery nor the target carries
// a :u: segment. Opaque-delivery platforms (ActiveEntryDelivery, e.g. chat-api)
// have no sub-scoping: the raw delivery id is the channel key and the finer
// levels are empty.
func BindKeysForRoute(route agentkit.RouteRef, policy RoutePolicy, userID string, session agentkit.SessionID) BindKeys {
	userID = strings.TrimSpace(userID)
	keys := BindKeys{Session: session}

	delivery, ok := RouteSessionID(route)
	if !ok || delivery == "" {
		return keys
	}
	if policy.ActiveEntryMode == ActiveEntryDelivery {
		keys.Channel = delivery
		return keys
	}

	target, _ := DecodeSessionRoute(route)
	channel := strings.TrimSpace(target.ChannelID)
	thread := strings.TrimSpace(target.ThreadID)
	scopeUser := strings.TrimSpace(target.ScopeUserID)

	if channel == "" {
		keys.Channel = delivery
		return keys
	}
	platform := strings.TrimSpace(route.Platform)
	base := agentkit.SessionID(platform + ":" + channel)
	keys.Channel = base
	if thread != "" {
		keys.Thread = agentkit.SessionID(string(base) + ":t:" + thread)
	}
	user := scopeUser
	if user == "" {
		user = userID
	}
	if user != "" {
		keys.User = agentkit.SessionID(string(base) + ":u:" + user)
	}
	return keys
}

// BindKeysFromContext derives keys from the turn envelope and route. Channel
// granularity is always the channel, independent of the configured conversation
// scope, so a channel-level default never fragments per thread. The user id is
// read from the envelope actor (UserIDFromContext), the same source the runner
// uses for route resolution, so the user level is populated even for a delivery
// that omitted the :u: segment.
func BindKeysFromContext(ctx context.Context) BindKeys {
	env := EnvelopeFromContext(ctx)
	platform := PlatformFromContext(ctx)
	if platform == "" {
		platform = strings.TrimSpace(env.Route.Platform)
	}
	policy := RoutePolicyForPlatform(platform, DefaultRoutePolicy(agentkit.SessionScopeChannel))
	return BindKeysForRoute(env.Route, policy, UserIDFromContext(ctx), SessionIDFromContext(ctx))
}
