package learning

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/workspace"
	"github.com/lengzhao/agentkit/runtime/session"
)

// SummarizeSessionUserMessages extracts recent user text from the current session.
func SummarizeSessionUserMessages(ctx context.Context, store agentkit.SessionStore, sessionID agentkit.SessionID, maxMessages int) (string, error) {
	if store == nil {
		return "", fmt.Errorf("session store is required")
	}
	if sessionID == "" {
		return "", fmt.Errorf("session id is required")
	}
	if maxMessages <= 0 {
		maxMessages = 8
	}
	sess, err := store.Get(ctx, sessionID)
	if err != nil {
		return "", err
	}
	events, err := session.ReadAllEvents(ctx, sess)
	if err != nil {
		return "", err
	}
	var users []string
	for _, ev := range events {
		if ev.Type != agentkit.EventUserMessage {
			continue
		}
		var msg agentkit.ModelMessage
		if err := json.Unmarshal(ev.Data, &msg); err != nil {
			continue
		}
		text := strings.TrimSpace(flattenMessageContent(msg.Content))
		if text == "" || strings.HasPrefix(text, "/") {
			continue
		}
		users = append(users, text)
	}
	if len(users) == 0 {
		return "", fmt.Errorf("no user messages in session")
	}
	if len(users) > maxMessages {
		users = users[len(users)-maxMessages:]
	}
	return strings.Join(users, " | "), nil
}

func flattenMessageContent(parts []agentkit.ContentPart) string {
	var b strings.Builder
	for _, part := range parts {
		if part.Type != "text" {
			continue
		}
		text := strings.TrimSpace(part.Text)
		if text == "" {
			continue
		}
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		b.WriteString(text)
	}
	return b.String()
}

// Service owns tenant personal memory and /learn commands.
type Service struct {
	disabled  bool
	charLimit int
	memoryRoot string
	memoryFile string
	workspace workspace.Service
	sessions  agentkit.SessionStore
}

type Config struct {
	Disabled   bool   `json:"disabled"`
	CharLimit  int    `json:"charLimit"`
	MemoryRoot string `json:"memoryRoot"`
	MemoryFile string `json:"memoryFile"`
}

type Deps struct {
	Workspace    workspace.Service     `json:"workspace"`
	SessionStore agentkit.SessionStore `json:"sessionStore"`
}

// New registers learning/default: Personal memory storage and /learn slash commands.
func New(cfg Config, deps Deps) (*Service, error) {
	if deps.Workspace == nil {
		return nil, fmt.Errorf("learning/default requires workspace")
	}
	if deps.SessionStore == nil {
		return nil, fmt.Errorf("learning/default requires sessionStore")
	}
	return &Service{
		disabled:   cfg.Disabled,
		charLimit:  cfg.CharLimit,
		memoryRoot: cfg.MemoryRoot,
		memoryFile: cfg.MemoryFile,
		workspace:  deps.Workspace,
		sessions:   deps.SessionStore,
	}, nil
}

func (s *Service) memoryStore(ctx context.Context) (*MemoryStore, error) {
	path, err := s.workspace.Resolve(ctx, MemoryRelPath(s.memoryRoot, s.memoryFile))
	if err != nil {
		return nil, err
	}
	limit := s.charLimit
	if limit <= 0 {
		limit = DefaultCharLimit
	}
	return NewMemoryStore(path, limit), nil
}

// LoadEntries reads personal memory for the current tenant workspace.
func (s *Service) LoadEntries(ctx context.Context) ([]MemoryEntry, int, int, error) {
	store, err := s.memoryStore(ctx)
	if err != nil {
		return nil, 0, 0, err
	}
	entries, err := store.Load()
	if err != nil {
		return nil, 0, 0, err
	}
	limit := store.CharLimit
	return entries, store.TotalChars(entries), limit, nil
}

func (s *Service) Commands() []agentkit.Command {
	return []agentkit.Command{learnCommand{svc: s}}
}

type learnCommand struct {
	svc *Service
}

func (learnCommand) Name() string  { return "learn" }
func (learnCommand) Alias() string { return "" }
func (learnCommand) Description() string {
	return "manage personal memory: show, append, remove, or learn from session"
}

func (c learnCommand) CommandExec(ctx context.Context, args string) (string, error) {
	if c.svc.disabled {
		return "", fmt.Errorf("learning is disabled in preset config")
	}
	fields := strings.Fields(strings.TrimSpace(args))
	if len(fields) == 0 {
		return c.svc.show(ctx)
	}
	switch strings.ToLower(fields[0]) {
	case "help", "-h", "--help":
		return FormatHelp(), nil
	case "memory":
		text := strings.TrimSpace(strings.Join(fields[1:], " "))
		if text == "" {
			return "", fmt.Errorf("usage: /learn memory <text>")
		}
		return c.svc.addMemory(ctx, text, "learn-memory")
	case "remove", "rm":
		text := strings.TrimSpace(strings.Join(fields[1:], " "))
		if text == "" {
			return "", fmt.Errorf("usage: /learn remove <text>")
		}
		return c.svc.removeMemory(ctx, text)
	case "session":
		return c.svc.learnSession(ctx)
	case "show":
		return c.svc.show(ctx)
	default:
		text := strings.TrimSpace(args)
		return c.svc.addMemory(ctx, text, "learn")
	}
}

func (s *Service) show(ctx context.Context) (string, error) {
	entries, used, limit, err := s.LoadEntries(ctx)
	if err != nil {
		return "", err
	}
	return FormatMemory(entries, used, limit), nil
}

func (s *Service) addMemory(ctx context.Context, text, source string) (string, error) {
	store, err := s.memoryStore(ctx)
	if err != nil {
		return "", err
	}
	if err := store.Add(text, source); err != nil {
		return "", err
	}
	entries, err := store.Load()
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("personal memory updated [%d/%d chars]", store.TotalChars(entries), store.CharLimit), nil
}

func (s *Service) removeMemory(ctx context.Context, text string) (string, error) {
	store, err := s.memoryStore(ctx)
	if err != nil {
		return "", err
	}
	if err := store.Remove(text); err != nil {
		return "", err
	}
	return "personal memory entry removed", nil
}

func (s *Service) learnSession(ctx context.Context) (string, error) {
	sessionID, _ := ctx.Value(agentkit.KeySessionID).(agentkit.SessionID)
	summary, err := SummarizeSessionUserMessages(ctx, s.sessions, sessionID, 8)
	if err != nil {
		return "", err
	}
	content := "Session notes: " + summary
	return s.addMemory(ctx, content, "learn-session")
}
