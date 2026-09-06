package session

import (
	"context"
	"fmt"
	"strings"

	"github.com/lengzhao/agentkit"
)

type CommandsConfig struct{}

type CommandsDeps struct {
	SessionStore agentkit.SessionStore `json:"sessionStore"`
}

// Commands contributes session lifecycle slash commands (/new, /session).
type Commands struct {
	store agentkit.SessionStore
}

// NewCommands registers session/commands: Session lifecycle slash commands backed by SessionStore.
func NewCommands(_ CommandsConfig, deps CommandsDeps) (agentkit.CommandProvider, error) {
	if deps.SessionStore == nil {
		return nil, fmt.Errorf("session/commands requires sessionStore")
	}
	return &Commands{store: deps.SessionStore}, nil
}

func (c *Commands) Commands() []agentkit.Command {
	return []agentkit.Command{
		newCommand{store: c.store},
		showSessionCommand{store: c.store},
	}
}

type newCommand struct {
	store agentkit.SessionStore
}

func (newCommand) Name() string        { return "new" }
func (newCommand) Alias() string       { return "" }
func (newCommand) Description() string { return "start a new conversation session" }

func (c newCommand) CommandExec(ctx context.Context, args string) (string, error) {
	if strings.TrimSpace(args) != "" {
		return "", fmt.Errorf("usage: /new")
	}
	entryKey := ActiveEntryKeyFromContext(ctx)
	if entryKey == "" {
		return "", fmt.Errorf("session id is required")
	}
	id := agentkit.SessionID(NewConversationID(string(entryKey)))
	activeStore, ok := c.store.(agentkit.ActiveSessionStore)
	if !ok {
		return "", fmt.Errorf("session store does not support active sessions")
	}
	if err := activeStore.SetActiveSession(ctx, entryKey, id); err != nil {
		return "", err
	}
	return string(id), nil
}

type showSessionCommand struct {
	store agentkit.SessionStore
}

func (showSessionCommand) Name() string  { return "session" }
func (showSessionCommand) Alias() string { return "sess" }
func (showSessionCommand) Description() string {
	return "show current session id, path, and message count"
}

func (c showSessionCommand) CommandExec(ctx context.Context, args string) (string, error) {
	if strings.TrimSpace(args) != "" {
		return "", fmt.Errorf("usage: /session")
	}
	env := EnvelopeFromContext(ctx)
	entryKey := ActiveEntryKeyFromContext(ctx)
	if entryKey == "" {
		entryKey = SessionIDFromContext(ctx)
	}
	sessionID, err := ResolveActiveSessionID(ctx, c.store, entryKey)
	if err != nil {
		return "", err
	}
	if sessionID == "" {
		return "", fmt.Errorf("session id is required")
	}
	sess, err := c.store.Get(ctx, sessionID)
	if err != nil {
		return "", err
	}
	events, err := ReadAllEvents(ctx, sess)
	if err != nil {
		return "", err
	}
	messages, err := sess.DeriveMessages(ctx)
	if err != nil {
		return "", err
	}

	var b strings.Builder
	if convID := MetadataString(env, MetadataConversationID); convID != "" {
		fmt.Fprintf(&b, "conversation id: %s\n", convID)
	}
	fmt.Fprintf(&b, "session id: %s\n", sessionID)
	fmt.Fprintf(&b, "path: %s\n", sessionPath(sess))
	if turns := MetadataString(env, MetadataTurnCount); turns != "" {
		fmt.Fprintf(&b, "turns: %s\n", turns)
	}
	fmt.Fprintf(&b, "events: %d\n", len(events))
	fmt.Fprintf(&b, "messages: %d", len(messages))
	return strings.TrimRight(b.String(), "\n"), nil
}

func sessionPath(sess agentkit.Session) string {
	if p, ok := sess.(FileBacked); ok {
		if path := p.FilePath(); path != "" {
			return path
		}
	}
	return "(memory)"
}
