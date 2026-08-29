package chatapi

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/tenant"
	"github.com/lengzhao/agentkit/cap/workspace"
)

const defaultSessionsDir = "sessions"

func (p *Platform) resolveConversation(ctx context.Context, channelKey, conversationID, createdBy string) (*conversation, error) {
	if c := p.conversations.findInChannel(channelKey, conversationID); c != nil {
		return c, nil
	}
	c, err := p.loadConversationFromSession(ctx, channelKey, conversationID, createdBy)
	if err != nil {
		return nil, err
	}
	if c == nil {
		c, err = p.conversationFromIndex(ctx, channelKey, conversationID, createdBy)
		if err != nil {
			return nil, err
		}
	}
	if c == nil {
		return nil, nil
	}
	p.conversations.register(c)
	return c, nil
}

func (p *Platform) listConversations(ctx context.Context, channelKey string, limit int) ([]*conversation, error) {
	if err := p.syncConversationsFromSessions(ctx, channelKey); err != nil {
		return nil, err
	}
	return p.conversations.listInChannelSorted(channelKey, limit), nil
}

func (p *Platform) syncConversationsFromSessions(ctx context.Context, channelKey string) error {
	if err := p.loadConversationIndex(ctx, channelKey); err != nil {
		return err
	}
	if p.sessionStore == nil || p.workspace == nil {
		return nil
	}
	dirs, err := p.sessionDirsForChannel(ctx, channelKey)
	if err != nil {
		return err
	}
	for _, dir := range dirs {
		if err := p.scanSessionDir(ctx, channelKey, dir); err != nil {
			return err
		}
	}
	return nil
}

func (p *Platform) scanSessionDir(ctx context.Context, channelKey, dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	prefix := sessionFilePrefix(channelKey)
	for _, ent := range entries {
		if ent.IsDir() {
			continue
		}
		name := ent.Name()
		if !strings.HasSuffix(name, ".jsonl") {
			continue
		}
		convID, ok := conversationIDFromSessionFile(channelKey, name, prefix)
		if !ok {
			continue
		}
		info, err := ent.Info()
		if err != nil {
			continue
		}
		if info.Size() == 0 {
			continue
		}
		c, err := p.loadConversationFromSession(ctx, channelKey, convID, "")
		if err != nil || c == nil {
			continue
		}
		if info.ModTime().After(c.UpdatedAt) {
			c.UpdatedAt = info.ModTime()
		}
		p.conversations.register(c)
	}
	return nil
}

func (p *Platform) sessionCtx(ctx context.Context, channelKey, conversationID string) context.Context {
	return context.WithValue(ctx, agentkit.KeySessionID, agentkit.SessionID(engineSessionKey(channelKey, conversationID)))
}

func (p *Platform) loadConversationFromSession(ctx context.Context, channelKey, conversationID, createdBy string) (*conversation, error) {
	if p.sessionStore == nil || !isOpaqueConversationID(conversationID) {
		return nil, nil
	}
	events, err := p.readDeliverySessionEvents(ctx, channelKey, conversationID)
	if err != nil || len(events) == 0 {
		return nil, err
	}
	created, updated := sessionEventBounds(events)
	turns := countUserMessages(events)
	return &conversation{
		ID:         conversationID,
		ChannelKey: channelKey,
		CreatedBy:  createdBy,
		CreatedAt:  created,
		UpdatedAt:  updated,
		TurnCount:  turns,
	}, nil
}

func (p *Platform) readDeliverySessionEvents(ctx context.Context, channelKey, conversationID string) ([]agentkit.SessionEvent, error) {
	sess, err := p.historySession(ctx, channelKey, conversationID, "")
	if err != nil || sess == nil {
		return nil, err
	}
	return sess.Read(p.sessionCtx(ctx, channelKey, conversationID), 0)
}

func (p *Platform) historySession(ctx context.Context, channelKey, conversationID, userID string) (agentkit.Session, error) {
	if p.sessionStore == nil {
		return nil, fmt.Errorf("chat-api: session store is required")
	}
	_ = userID
	tenantCtx := p.sessionCtx(ctx, channelKey, conversationID)
	sessionID := agentkit.SessionID(engineSessionKey(channelKey, conversationID))
	sess, err := p.sessionStore.Get(tenantCtx, sessionID)
	if err != nil {
		return nil, err
	}
	events, err := sess.Read(tenantCtx, 0)
	if err != nil {
		return nil, err
	}
	if len(events) == 0 {
		return nil, nil
	}
	return sess, nil
}

func (p *Platform) sessionDirsForChannel(ctx context.Context, channelKey string) ([]string, error) {
	rel := strings.TrimSpace(p.sessionsDirRel)
	if rel == "" {
		rel = defaultSessionsDir
	}
	abs, err := p.sessionsDirForChannel(ctx, channelKey, rel)
	if err != nil {
		return nil, err
	}
	return []string{abs}, nil
}

func (p *Platform) sessionsDirForChannel(ctx context.Context, channelKey, rel string) (string, error) {
	if p.workspace == nil {
		return "", fmt.Errorf("chat-api: workspace is required")
	}
	ctx = context.WithValue(ctx, agentkit.KeySessionID, agentkit.SessionID(engineSessionKey(channelKey, "conv_probe")))
	return p.workspace.Resolve(ctx, rel)
}

func conversationIDFromSessionFile(_, filename, prefix string) (string, bool) {
	base := strings.TrimSuffix(filename, ".jsonl")
	if !strings.HasPrefix(base, prefix) {
		return "", false
	}
	rest := strings.TrimPrefix(base, prefix)
	if !isOpaqueConversationID(rest) {
		return "", false
	}
	return rest, true
}

func safeSessionFileSegment(raw string) string {
	var b strings.Builder
	for _, r := range raw {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return b.String()
}

func countUserMessages(events []agentkit.SessionEvent) int {
	n := 0
	for _, ev := range events {
		if ev.Type == agentkit.EventUserMessage {
			n++
		}
	}
	return n
}

func sessionEventBounds(events []agentkit.SessionEvent) (created, updated time.Time) {
	now := time.Now()
	created, updated = now, now
	for _, ev := range events {
		if !ev.CreatedAt.IsZero() {
			if time.Time.Equal(created, now) || ev.CreatedAt.Before(created) {
				created = ev.CreatedAt
			}
			if ev.CreatedAt.After(updated) {
				updated = ev.CreatedAt
			}
		}
	}
	return created, updated
}

// staticWorkspace is used in tests.
type staticWorkspace struct {
	root string
}

func (s staticWorkspace) Resolve(_ context.Context, rel string) (string, error) {
	return workspace.ResolveRel(s.root, rel)
}

// tenantStaticWorkspace resolves local: paths under LocalBase/<tenantDir>/.
type tenantStaticWorkspace struct {
	localBase          string
	omitPlatformPrefix bool
}

func (s tenantStaticWorkspace) Resolve(ctx context.Context, rel string) (string, error) {
	dir := tenant.LocalDirName(tenant.FromContext(ctx), s.omitPlatformPrefix)
	if dir == "" {
		dir = "_default"
	}
	return workspace.ResolveRel(filepath.Join(s.localBase, dir), rel)
}

func tenantWorkspaceRoot(localBase string) workspace.Service {
	return tenantStaticWorkspace{localBase: localBase}
}
