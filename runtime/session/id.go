package session

import (
	"time"

	"github.com/lengzhao/agentkit"
)

const DefaultCLISessionID = agentkit.SessionID("cli:default")

// NewCLISessionID returns a fresh opaque CLI session id.
func NewCLISessionID() agentkit.SessionID {
	return agentkit.SessionID("cli:" + time.Now().UTC().Format("20060102-150405.000"))
}

// SessionScope selects how one group's messages map to sessions. It is the
// platform's decision and is deliberately independent of the working directory:
// every scope below derives the same cap/tenant key, so a channel keeps one
// workdir no matter how finely its history is split.
type SessionScope string

const (
	// ScopeChannel gives the whole channel a single session. Everyone in the
	// group shares one history and is told apart by per-message user
	// attribution, which is what makes "两个人在同一个群里接着聊" work.
	ScopeChannel SessionScope = "channel"
	// ScopeThread gives each thread its own session, with the channel's
	// top-level messages sharing one. This is the usual choice for a busy
	// channel where unrelated tasks run side by side.
	ScopeThread SessionScope = "thread"
	// ScopeUser gives each person their own session inside the channel: shared
	// working directory, private history.
	ScopeUser SessionScope = "user"
)

// SlackSessionID builds a stable session key from Slack channel and optional thread timestamp.
// Use threadTS from event.ThreadTimeStamp; for top-level channel messages pass "".
func SlackSessionID(channelID, threadTS string) agentkit.SessionID {
	if threadTS == "" {
		return agentkit.SessionID("slack:" + channelID)
	}
	return agentkit.SessionID("slack:" + channelID + ":t:" + threadTS)
}

// SlackSessionIDForScope builds the session key for a given scope. Arguments not
// used by the scope are ignored, so a platform can pass everything it has and
// let the configured scope decide.
//
// An unknown scope falls back to ScopeThread rather than erroring: over-sharing
// a session is a privacy problem, and thread scope is the narrower default of
// the two candidates a typo could land between.
func SlackSessionIDForScope(scope SessionScope, channelID, threadTS, userID string) agentkit.SessionID {
	switch scope {
	case ScopeChannel:
		return agentkit.SessionID("slack:" + channelID)
	case ScopeUser:
		if userID == "" {
			return agentkit.SessionID("slack:" + channelID)
		}
		return agentkit.SessionID("slack:" + channelID + ":u:" + userID)
	default:
		return SlackSessionID(channelID, threadTS)
	}
}
