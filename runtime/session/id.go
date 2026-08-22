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

// SlackSessionID builds a stable session key from Slack channel and optional thread timestamp.
// Use threadTS from event.ThreadTimeStamp; for top-level channel messages pass "".
func SlackSessionID(channelID, threadTS string) agentkit.SessionID {
	if threadTS == "" {
		return agentkit.SessionID("slack:" + channelID)
	}
	return agentkit.SessionID("slack:" + channelID + ":thread:" + threadTS)
}
