package schedule

import (
	"strings"
)

const (
	SessionModeStateless = "stateless"
	SessionModeReuse     = "reuse"
	SessionModeFresh     = "fresh" // alias of stateless
	SessionModeFixed     = "fixed"
)

// FireMeta returns schedule fire metadata when present on an inbound event.
func FireMeta(metadata map[string]any) (map[string]any, bool) {
	if len(metadata) == 0 {
		return nil, false
	}
	raw, ok := metadata["schedule"].(map[string]any)
	if !ok || len(raw) == 0 {
		return nil, false
	}
	return raw, true
}

// SessionModeFromMeta reads sessionMode from schedule fire metadata.
func SessionModeFromMeta(meta map[string]any) string {
	if len(meta) == 0 {
		return ""
	}
	mode, _ := meta["sessionMode"].(string)
	return strings.ToLower(strings.TrimSpace(mode))
}

// IsStatelessSessionMode reports whether a schedule turn should avoid inheriting
// the delivery conversation's active session history.
func IsStatelessSessionMode(mode string) bool {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "", SessionModeStateless, SessionModeFresh:
		return true
	default:
		return false
	}
}

// IsFireTurn reports whether metadata marks a schedule-fired inbound turn.
func IsFireTurn(metadata map[string]any) bool {
	meta, ok := FireMeta(metadata)
	if !ok {
		return false
	}
	fired, _ := meta["fired"].(bool)
	return fired
}

// IsFireStateless reports whether a schedule-fired inbound event should run in a
// fresh side session without parent history.
func IsFireStateless(metadata map[string]any) bool {
	meta, ok := FireMeta(metadata)
	if !ok {
		return false
	}
	fired, _ := meta["fired"].(bool)
	if !fired {
		return false
	}
	return IsStatelessSessionMode(SessionModeFromMeta(meta))
}
