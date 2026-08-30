package session

import (
	"context"
	"fmt"
	"strings"

	"github.com/lengzhao/agentkit"
)

func (s *Store) Commands() []agentkit.Command {
	return []agentkit.Command{
		newCommand{store: s},
		showSessionCommand{store: s},
	}
}

type newCommand struct {
	store *Store
}

func (newCommand) Name() string        { return "new" }
func (newCommand) Alias() string       { return "" }
func (newCommand) Description() string { return "start a new CLI session" }

func (c newCommand) CommandExec(ctx context.Context, args string) (string, error) {
	if strings.TrimSpace(args) != "" {
		return "", fmt.Errorf("usage: /new")
	}
	current, _ := ctx.Value(agentkit.KeySessionID).(agentkit.SessionID)
	id := NewSessionID(current)
	if c.store != nil {
		if isCLISessionID(current) {
			if err := c.store.SetCLICurrent(ctx, id); err != nil {
				return "", err
			}
		} else if current != "" {
			if err := c.store.SetActiveSession(ctx, current, id); err != nil {
				return "", err
			}
		}
	}
	return string(id), nil
}

func isCLISessionID(id agentkit.SessionID) bool {
	return id == "" || strings.HasPrefix(string(id), "cli:")
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
	sessionID, _ := ctx.Value(agentkit.KeySessionID).(agentkit.SessionID)
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
	fmt.Fprintf(&b, "session id: %s\n", sessionID)
	fmt.Fprintf(&b, "path: %s\n", sessionPath(sess))
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
