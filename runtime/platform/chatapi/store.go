package chatapi

import (
	"sync"
	"time"

	"github.com/lengzhao/agentkit"
)

type conversation struct {
	ID         string
	ChannelKey string
	Name       string
	CreatedBy  string
	AgentID    agentkit.AgentID
	CreatedAt  time.Time
	UpdatedAt  time.Time
	TurnCount  int
}

type conversationStore struct {
	mu   sync.RWMutex
	byID map[string]*conversation
}

func newConversationStore() *conversationStore {
	return &conversationStore{byID: make(map[string]*conversation)}
}

func (s *conversationStore) create(channelKey, createdBy string) (*conversation, error) {
	id, err := newConversationID()
	if err != nil {
		return nil, err
	}
	now := time.Now()
	c := &conversation{
		ID:         id,
		ChannelKey: channelKey,
		Name:       "",
		CreatedBy:  createdBy,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	s.mu.Lock()
	s.byID[id] = c
	s.mu.Unlock()
	return c, nil
}

func (s *conversationStore) get(id string) *conversation {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.byID[id]
}

func (s *conversationStore) findInChannel(channelKey, id string) *conversation {
	s.mu.RLock()
	defer s.mu.RUnlock()
	c, ok := s.byID[id]
	if !ok || c.ChannelKey != channelKey {
		return nil
	}
	return c
}

func (s *conversationStore) register(c *conversation) {
	if c == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	existing, ok := s.byID[c.ID]
	if !ok {
		cp := *c
		s.byID[c.ID] = &cp
		return
	}
	if c.TurnCount > existing.TurnCount {
		existing.TurnCount = c.TurnCount
	}
	if c.UpdatedAt.After(existing.UpdatedAt) {
		existing.UpdatedAt = c.UpdatedAt
	}
	if existing.CreatedBy == "" && c.CreatedBy != "" {
		existing.CreatedBy = c.CreatedBy
	}
	if existing.CreatedAt.After(c.CreatedAt) && !c.CreatedAt.IsZero() {
		existing.CreatedAt = c.CreatedAt
	}
}

func (s *conversationStore) listInChannelSorted(channelKey string, limit int) []*conversation {
	s.mu.RLock()
	out := make([]*conversation, 0)
	for _, c := range s.byID {
		if c.ChannelKey == channelKey {
			out = append(out, c)
		}
	}
	s.mu.RUnlock()
	sortConversationsByUpdated(out)
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

func sortConversationsByUpdated(list []*conversation) {
	for i := 0; i < len(list); i++ {
		for j := i + 1; j < len(list); j++ {
			if list[j].UpdatedAt.After(list[i].UpdatedAt) {
				list[i], list[j] = list[j], list[i]
			}
		}
	}
}

func (s *conversationStore) listInChannel(channelKey string, limit int) []*conversation {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*conversation, 0)
	for _, c := range s.byID {
		if c.ChannelKey == channelKey {
			out = append(out, c)
		}
	}
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

func (s *conversationStore) bumpTurn(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if c, ok := s.byID[id]; ok {
		c.TurnCount++
		c.UpdatedAt = time.Now()
	}
}

func toConversationView(c *conversation) map[string]any {
	if c == nil {
		return nil
	}
	view := map[string]any{
		"id":         c.ID,
		"name":       c.Name,
		"created_at": c.CreatedAt.Unix(),
		"updated_at": c.UpdatedAt.Unix(),
	}
	if id := c.agentID(); id != "" {
		view["agent_id"] = string(id)
	}
	return view
}
