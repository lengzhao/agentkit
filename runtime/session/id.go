package session

import (
	"strings"
	"time"

	"github.com/lengzhao/agentkit"
)

const DefaultCLISessionID = agentkit.SessionID("cli:default")

// NewCLISessionID returns a fresh opaque CLI session id.
func NewCLISessionID() agentkit.SessionID {
	return agentkit.SessionID("cli:" + time.Now().UTC().Format("20060102-150405.000"))
}

// NewSessionID returns a fresh logical session id derived from the current
// stable session key. CLI keeps its historic cli:<timestamp> format.
func NewSessionID(base agentkit.SessionID) agentkit.SessionID {
	if strings.HasPrefix(string(base), "cli:") || base == "" {
		return NewCLISessionID()
	}
	return agentkit.SessionID(string(base) + ":new:" + time.Now().UTC().Format("20060102-150405.000"))
}

// SlackSessionID builds a stable session key from Slack channel and optional thread timestamp.
// Use threadTS from event.ThreadTimeStamp; for top-level channel messages pass "".
func SlackSessionID(channelID, threadTS string) agentkit.SessionID {
	if threadTS == "" {
		return agentkit.SessionID("slack:" + channelID)
	}
	return agentkit.SessionID("slack:" + channelID + ":t:" + threadTS)
}

// SlackSessionIDForScope builds the effective session id for Slack components.
// Prefer ApplyScope(BuildDeliverySessionID(...), scope, userID) in new code;
// this helper remains for tests and direct agent invocations.
func SlackSessionIDForScope(scope SessionScope, channelID, threadTS, userID string) agentkit.SessionID {
	delivery := BuildDeliverySessionID("slack", channelID, threadTS, userID)
	if delivery == "" {
		return ""
	}
	return ApplyScope(delivery, scope, userID)
}
