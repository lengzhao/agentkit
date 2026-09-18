package runner

import (
	"context"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/rctx"
	"github.com/lengzhao/agentkit/runtime/session"
)

// busyStopSession finds which session id currently has an in-flight turn for this
// slash / inbound routing context. IM platforms may key turns on scoped delivery,
// active /new children, or runner vs platform sessionScope mismatches.
func busyStopSession(ctx context.Context, store agentkit.SessionStore, loop agentkit.Loop) (agentkit.SessionID, bool, error) {
	ids, err := stopCandidateSessionIDs(ctx, store)
	if err != nil {
		return "", false, err
	}
	for _, id := range ids {
		if id == "" {
			continue
		}
		if loop.IsSessionBusy(id) {
			return id, true, nil
		}
	}
	return "", false, nil
}

func stopCandidateSessionIDs(ctx context.Context, store agentkit.SessionStore) ([]agentkit.SessionID, error) {
	var out []agentkit.SessionID
	add := func(id agentkit.SessionID) {
		if id == "" {
			return
		}
		for _, existing := range out {
			if existing == id {
				return
			}
		}
		out = append(out, id)
	}

	entry := session.ActiveEntryKeyFromContext(ctx)
	if entry == "" {
		entry = rctx.SessionIDFromContext(ctx)
	}
	conv := rctx.SessionIDFromContext(ctx)
	add(entry)
	add(conv)

	if store != nil {
		for _, key := range []agentkit.SessionID{entry, conv} {
			if key == "" {
				continue
			}
			resolved, err := session.ResolveActiveSessionID(ctx, store, key)
			if err != nil {
				return nil, err
			}
			add(resolved)
		}
	}

	env := rctx.EnvelopeFromContext(ctx)
	userID := rctx.UserIDFromContext(ctx)
	platform := rctx.PlatformFromContext(ctx)
	delivery, ok := session.RouteSessionID(env.Route)
	if ok && delivery != "" {
		for _, scope := range []session.SessionScope{
			session.ScopeChannel,
			session.ScopeUser,
			session.ScopeThread,
		} {
			policy := session.RoutePolicyForPlatform(platform, session.DefaultRoutePolicy(scope))
			ek := session.ActiveEntryKey(env.Route, policy, userID)
			add(ek)
			add(session.ApplyScope(delivery, scope, userID))
			if store != nil && ek != "" {
				resolved, err := session.ResolveActiveSessionID(ctx, store, ek)
				if err != nil {
					return nil, err
				}
				add(resolved)
			}
		}
	}

	return out, nil
}
