package session

import (
	"strings"

	"github.com/lengzhao/agentkit"
)

// DefaultSessionScope is the runner default when sessionScope is unset.
const DefaultSessionScope = agentkit.DefaultSessionScope

// SessionScope selects how delivery SessionIDs collapse for Loop scheduling
// and session history.
type SessionScope = agentkit.SessionScope

const (
	ScopeChannel = agentkit.SessionScopeChannel
	ScopeThread  = agentkit.SessionScopeThread
	ScopeUser    = agentkit.SessionScopeUser
)

// ParseScope normalizes runner config. Unknown values fall back to channel scope.
func ParseScope(raw string) SessionScope {
	switch SessionScope(strings.ToLower(strings.TrimSpace(raw))) {
	case ScopeThread:
		return ScopeThread
	case ScopeUser:
		return ScopeUser
	case ScopeChannel:
		return ScopeChannel
	default:
		return DefaultSessionScope
	}
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
func ApplyScope(delivery agentkit.SessionID, scope SessionScope, userID string) agentkit.SessionID {
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

func (p DeliveryParts) effective(scope SessionScope) agentkit.SessionID {
	base := p.Platform + ":" + p.Channel
	switch scope {
	case ScopeChannel:
		return agentkit.SessionID(base)
	case ScopeUser:
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
