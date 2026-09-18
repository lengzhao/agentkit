package runner

import (
	"context"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/rctx"
	sessstore "github.com/lengzhao/agentkit/runtime/session/sessstore"
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

	entry := rctx.ActiveEntryKeyFromContext(ctx)
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
			resolved, err := sessstore.ResolveActiveSessionID(ctx, store, key)
			if err != nil {
				return nil, err
			}
			add(resolved)
		}
	}

	env := rctx.EnvelopeFromContext(ctx)
	userID := rctx.UserIDFromContext(ctx)
	platform := rctx.PlatformFromContext(ctx)
	delivery, ok := rctx.RouteSessionID(env.Route)
	if ok && delivery != "" {
		for _, scope := range []sessstore.SessionScope{
			sessstore.ScopeChannel,
			sessstore.ScopeUser,
			sessstore.ScopeThread,
		} {
			policy := rctx.RoutePolicyForPlatform(platform, rctx.DefaultRoutePolicy(scope))
			ek := rctx.ActiveEntryKey(env.Route, policy, userID)
			add(ek)
			add(rctx.ApplyScope(delivery, scope, userID))
			if store != nil && ek != "" {
				resolved, err := sessstore.ResolveActiveSessionID(ctx, store, ek)
				if err != nil {
					return nil, err
				}
				add(resolved)
			}
		}
	}

	return out, nil
}
