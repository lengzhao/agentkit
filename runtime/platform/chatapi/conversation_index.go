package chatapi

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/lengzhao/agentkit"
)

const conversationIndexDir = "chat-api/conversations"

type persistedConversation struct {
	ID        string `json:"id"`
	CreatedBy string `json:"created_by,omitempty"`
	CreatedAt int64  `json:"created_at"`
	UpdatedAt int64  `json:"updated_at"`
	TurnCount int    `json:"turn_count"`
	AgentID   string `json:"agent_id,omitempty"`
}

type conversationIndexFile struct {
	Conversations []persistedConversation `json:"conversations"`
}

func conversationIndexPath(channelKey string) string {
	return filepath.Join(conversationIndexDir, safeSessionFileSegment(channelKey)+".json")
}

func (p *Platform) loadConversationIndex(ctx context.Context, channelKey string) error {
	if p.workspace == nil {
		return nil
	}
	path, err := p.indexPathForChannel(ctx, channelKey)
	if err != nil {
		return err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var idx conversationIndexFile
	if err := json.Unmarshal(raw, &idx); err != nil {
		return fmt.Errorf("chat-api: parse conversation index: %w", err)
	}
	for _, item := range idx.Conversations {
		if !isOpaqueConversationID(item.ID) {
			continue
		}
		c := &conversation{
			ID:         item.ID,
			ChannelKey: channelKey,
			CreatedBy:  item.CreatedBy,
			CreatedAt:  time.Unix(item.CreatedAt, 0),
			UpdatedAt:  time.Unix(item.UpdatedAt, 0),
			TurnCount:  item.TurnCount,
			AgentID:    agentkit.AgentID(item.AgentID),
		}
		p.conversations.register(c)
	}
	return nil
}

func (p *Platform) saveConversationIndex(ctx context.Context, channelKey string) error {
	if p.workspace == nil {
		return nil
	}
	path, err := p.indexPathForChannel(ctx, channelKey)
	if err != nil {
		return err
	}
	list := p.conversations.listInChannel(channelKey, 0)
	items := make([]persistedConversation, 0, len(list))
	for _, c := range list {
		if c == nil || !isOpaqueConversationID(c.ID) {
			continue
		}
		items = append(items, persistedConversation{
			ID:        c.ID,
			CreatedBy: c.CreatedBy,
			CreatedAt: c.CreatedAt.Unix(),
			UpdatedAt: c.UpdatedAt.Unix(),
			TurnCount: c.TurnCount,
			AgentID:   string(c.agentID()),
		})
	}
	raw, err := json.MarshalIndent(conversationIndexFile{Conversations: items}, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func (p *Platform) indexPathForChannel(ctx context.Context, channelKey string) (string, error) {
	ctx = p.sessionCtx(ctx, channelKey, "conv_probe")
	return p.workspace.Resolve(ctx, conversationIndexPath(channelKey))
}

func (p *Platform) createConversation(ctx context.Context, channelKey, createdBy string) (*conversation, error) {
	c, err := p.conversations.create(channelKey, createdBy)
	if err != nil {
		return nil, err
	}
	p.persistConversationIndex(ctx, channelKey)
	return c, nil
}

func (p *Platform) persistConversationIndex(ctx context.Context, channelKey string) {
	if err := p.saveConversationIndex(ctx, channelKey); err != nil {
		slog.Warn("chat-api: save conversation index", "channel", channelKey, "err", err)
	}
}

func (p *Platform) conversationFromIndex(ctx context.Context, channelKey, conversationID, createdBy string) (*conversation, error) {
	if p.workspace == nil || !isOpaqueConversationID(conversationID) {
		return nil, nil
	}
	path, err := p.indexPathForChannel(ctx, channelKey)
	if err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var idx conversationIndexFile
	if err := json.Unmarshal(raw, &idx); err != nil {
		return nil, err
	}
	for _, item := range idx.Conversations {
		if item.ID != conversationID {
			continue
		}
		cb := item.CreatedBy
		if cb == "" {
			cb = createdBy
		}
		return &conversation{
			ID:         item.ID,
			ChannelKey: channelKey,
			CreatedBy:  cb,
			CreatedAt:  time.Unix(item.CreatedAt, 0),
			UpdatedAt:  time.Unix(item.UpdatedAt, 0),
			TurnCount:  item.TurnCount,
			AgentID:    agentkit.AgentID(item.AgentID),
		}, nil
	}
	return nil, nil
}
