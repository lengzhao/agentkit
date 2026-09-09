package session

import (
	"strings"

	"github.com/lengzhao/agentkit"
)

// WorkspaceKey derives the workspace isolation key from an opaque SessionID.
// Routable IM-style ids collapse to platform:channel; non-routable ids keep
// platform plus the first segment after the platform prefix.
func WorkspaceKey(sessionID string) string {
	id := strings.TrimSpace(sessionID)
	if id == "" {
		return ""
	}
	parts := ParseDelivery(agentkit.SessionID(id), "")
	if parts.Routable {
		if parts.Channel == "" {
			return parts.Platform
		}
		return parts.Platform + ":" + parts.Channel
	}
	platform, rest, ok := strings.Cut(id, ":")
	if !ok || platform == "" {
		return id
	}
	segment, _, _ := strings.Cut(rest, ":")
	if segment == "" {
		return platform
	}
	return platform + ":" + segment
}

func workspaceRoutingSegment(key string) string {
	key = strings.TrimSpace(key)
	if key == "" {
		return ""
	}
	_, rest, ok := strings.Cut(key, ":")
	if !ok {
		return key
	}
	segment, _, _ := strings.Cut(rest, ":")
	return segment
}

// WorkspaceDirName maps a workspace key to a single safe path segment.
func WorkspaceDirName(key string) string {
	return WorkspaceLocalDirName(key, false)
}

// WorkspaceLocalDirName maps a workspace key to a single safe path segment under
// localBase. When omitPlatform is true, only the first routing segment is used.
func WorkspaceLocalDirName(key string, omitPlatform bool) string {
	if key == "" {
		return ""
	}
	if omitPlatform {
		key = workspaceRoutingSegment(key)
		if key == "" {
			return ""
		}
	}
	return sanitizeWorkspaceDirSegment(key)
}

func sanitizeWorkspaceDirSegment(key string) string {
	var b strings.Builder
	b.Grow(len(key))
	prevDot := false
	for _, r := range key {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
			prevDot = false
		case r == '.':
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
