package common

import (
	"sync"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/permission"
)

// InteractionOptions remembers option labels for platforms with short callback payloads (Telegram).
type InteractionOptions struct {
	mu sync.Map
}

type interactionOpts struct {
	requestID string
	replies   []permission.Reply
}

func (s *InteractionOptions) Set(sessionID agentkit.SessionID, requestID string, replies []permission.Reply) {
	if sessionID == "" || requestID == "" {
		return
	}
	cp := append([]permission.Reply(nil), replies...)
	s.mu.Store(sessionID, interactionOpts{requestID: requestID, replies: cp})
}

func (s *InteractionOptions) Reply(sessionID agentkit.SessionID, requestID, userID string, index int) (permission.Reply, bool) {
	raw, ok := s.mu.Load(sessionID)
	if !ok {
		return permission.Reply{}, false
	}
	entry := raw.(interactionOpts)
	if entry.requestID != requestID || index < 0 || index >= len(entry.replies) {
		return permission.Reply{}, false
	}
	reply := entry.replies[index]
	reply.UserID = userID
	return reply, true
}

func (s *InteractionOptions) Clear(sessionID agentkit.SessionID) {
	s.mu.Delete(sessionID)
}
