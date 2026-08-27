package session

import (
	"strings"

	"github.com/lengzhao/agentkit"
)

// DefaultSessionScope is the runner default when sessionScope is unset.
const DefaultSessionScope = ScopeChannel

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

// BuildDeliverySessionID is the canonical finest-grain id platforms should emit.
// Runner applies sessionScope to collapse it for scheduling and history.
//
// Examples:
//
//	slack:C001
//	slack:C001:t:1712345678.9
//	slack:C001:t:1712345678.9:u:U456
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
// and permission pending. delivery is the platform-owned routing id; userID
// fills in a missing :u: segment when present on the event envelope.
func ApplyScope(delivery agentkit.SessionID, scope SessionScope, userID string) agentkit.SessionID {
	id := strings.TrimSpace(string(delivery))
	if id == "" {
		return delivery
	}
	parts := parseDelivery(id, userID)
	if !parts.routable {
		return delivery
	}
	return parts.effective(ParseScope(string(scope)))
}

type deliveryParts struct {
	platform string
	channel  string
	thread   string
	user     string
	routable bool
}

func parseDelivery(id, fallbackUser string) deliveryParts {
	segments := strings.Split(id, ":")
	if len(segments) < 2 {
		return deliveryParts{routable: false}
	}

	p := deliveryParts{
		platform: segments[0],
		channel:  segments[1],
		routable: true,
	}

	// Single-segment transports (cli:default, sub:...) keep their id verbatim.
	switch p.platform {
	case "cli", "sub", "jsonl", "worker", "timer", "cron":
		if len(segments) == 2 {
			p.routable = false
			return p
		}
	}

	for i := 2; i < len(segments); {
		switch segments[i] {
		case "t":
			if i+1 >= len(segments) {
				i++
				continue
			}
			p.thread = segments[i+1]
			i += 2
		case "u":
			if i+1 >= len(segments) {
				i++
				continue
			}
			p.user = segments[i+1]
			i += 2
		default:
			// Legacy slack user id without :u: marker: slack:C001:U456
			if p.thread == "" && p.user == "" && i == 2 && len(segments) == 3 {
				p.user = segments[i]
			}
			i++
		}
	}

	if p.user == "" {
		p.user = strings.TrimSpace(fallbackUser)
	}
	return p
}

func (p deliveryParts) effective(scope SessionScope) agentkit.SessionID {
	base := p.platform + ":" + p.channel
	switch scope {
	case ScopeChannel:
		return agentkit.SessionID(base)
	case ScopeUser:
		if p.user == "" {
			return agentkit.SessionID(base)
		}
		return agentkit.SessionID(base + ":u:" + p.user)
	default: // ScopeThread
		if p.thread == "" {
			return agentkit.SessionID(base)
		}
		return agentkit.SessionID(base + ":t:" + p.thread)
	}
}
