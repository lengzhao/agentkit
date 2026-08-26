// Package tenant derives the isolation unit a conversation belongs to.
//
// A tenant is coarser than a session: one Slack channel is one tenant, but that
// channel may hold many sessions (one per thread, or one per user). The split
// matters because the two answer different questions:
//
//   - SessionID decides which conversation history a turn appends to.
//   - Tenant decides which working directory that turn runs in.
//
// Keeping them separate is what lets a channel share one workdir across all its
// threads while each thread keeps its own history.
package tenant

import (
	"context"
	"strings"

	"github.com/lengzhao/agentkit"
)

// FromContext reports the tenant behind the current session, or "" when the
// context carries no session id. Anything that needs per-tenant state — a
// workspace root, a client pool slot — keys off this rather than re-deriving it.
func FromContext(ctx context.Context) string {
	id, _ := ctx.Value(agentkit.KeySessionID).(agentkit.SessionID)
	return Key(string(id))
}

// Key derives the tenant key from an opaque SessionID by a fixed rule: the
// platform segment plus the first routing segment.
//
//	slack:C123ABC              -> slack:C123ABC
//	slack:C123ABC:U456         -> slack:C123ABC
//	slack:C123ABC:t:171234.56  -> slack:C123ABC
//	cli:default                -> cli:default
//	                           -> "" (no session, no tenant)
//
// Every Slack session scope therefore lands on the same tenant for a given
// channel, which is why session granularity and workdir granularity can be
// chosen independently.
//
// This is the only place the rule lives. Platforms stay free to encode whatever
// they like after the second segment; nothing here parses further.
func Key(sessionID string) string {
	id := strings.TrimSpace(sessionID)
	if id == "" {
		return ""
	}
	platform, rest, ok := strings.Cut(id, ":")
	if !ok || platform == "" {
		// No platform prefix: treat the whole id as its own tenant rather than
		// silently collapsing unknown ids into a shared root.
		return id
	}
	segment, _, _ := strings.Cut(rest, ":")
	if segment == "" {
		return platform
	}
	return platform + ":" + segment
}

// DirName maps a tenant key to a single safe path segment. Keys carry ':' and
// platform-chosen characters; a tenant root must never be able to reach a
// sibling tenant, so anything outside [A-Za-z0-9._-] becomes '_' and a run of
// dots can not survive.
func DirName(key string) string {
	if key == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(key))
	prevDot := false
	for _, r := range key {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
			prevDot = false
		case r == '.':
			// A lone '.' is fine; a run of them is how you climb out of a root.
			if prevDot {
				b.WriteByte('_')
				continue
			}
			b.WriteByte('.')
			prevDot = true
		default:
			b.WriteByte('_')
			prevDot = false
		}
	}
	name := b.String()
	if name == "" || name == "." {
		return "_"
	}
	return name
}
