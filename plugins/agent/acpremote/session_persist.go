package acpremote

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"

	acp "github.com/coder/acp-go-sdk"
	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/filesystem"
)

// defaultBindDir is the tenant-relative dir holding ACP session resume binds.
// Owned by this plugin; independent of the session store backend layout.
const defaultBindDir = "acp"
const maxReplayMessages = 40

type acpSessionBind struct {
	AgentID      agentkit.AgentID `json:"agentId"`
	ACPSessionID acp.SessionId    `json:"acpSessionId"`
	Cwd          string           `json:"cwd"`
}

func acpSessionBindPath(bindDir string, sessionID agentkit.SessionID, agentID agentkit.AgentID) (string, error) {
	sessName, err := safeNameForFile(string(sessionID))
	if err != nil {
		return "", err
	}
	agentName, err := safeNameForFile(string(agentID))
	if err != nil {
		return "", err
	}
	return strings.TrimSuffix(bindDir, "/") + "/" + sessName + "/acp-session." + agentName + ".json", nil
}

func safeNameForFile(raw string) (string, error) {
	if raw == "" {
		return "", fmt.Errorf("empty id")
	}
	if strings.Contains(raw, "..") {
		return "", fmt.Errorf("invalid id")
	}
	var b strings.Builder
	for _, r := range raw {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return b.String(), nil
}

func loadACPSessionBind(ctx context.Context, fs filesystem.Service, path string) (acpSessionBind, bool, error) {
	data, err := fs.Read(ctx, path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return acpSessionBind{}, false, nil
		}
		return acpSessionBind{}, false, err
	}
	var bind acpSessionBind
	if err := json.Unmarshal(data, &bind); err != nil {
		return acpSessionBind{}, false, err
	}
	if bind.ACPSessionID == "" {
		return acpSessionBind{}, false, nil
	}
	return bind, true, nil
}

func saveACPSessionBind(ctx context.Context, fs filesystem.Service, path string, bind acpSessionBind) error {
	raw, err := json.Marshal(bind)
	if err != nil {
		return err
	}
	return fs.Write(ctx, path, raw)
}

func priorMessages(messages []agentkit.ModelMessage) []agentkit.ModelMessage {
	if len(messages) > 0 && messages[len(messages)-1].Role == "user" {
		messages = messages[:len(messages)-1]
	}
	if len(messages) > maxReplayMessages {
		messages = messages[len(messages)-maxReplayMessages:]
	}
	return messages
}

func (b *bridge) replayHistory(ctx context.Context, sess agentkit.Session, acpSessionID acp.SessionId) error {
	messages, err := sess.DeriveMessages(ctx)
	if err != nil {
		return err
	}
	messages = priorMessages(messages)
	if len(messages) == 0 {
		return nil
	}

	var prompt strings.Builder
	prompt.WriteString("The following is conversation history from before a session reconnect. Continue from this context.\n\n")
	for _, msg := range messages {
		text := strings.TrimSpace(messageText(msg))
		if text == "" {
			continue
		}
		switch msg.Role {
		case "user":
			prompt.WriteString("User: ")
		case "assistant":
			prompt.WriteString("Assistant: ")
		default:
			continue
		}
		prompt.WriteString(text)
		prompt.WriteString("\n\n")
	}
	if prompt.Len() == 0 {
		return nil
	}

	slog.Info("acp-remote: replaying session history", "messages", len(messages))
	_, err = b.prompt(ctx, acpSessionID, []acp.ContentBlock{acp.TextBlock(strings.TrimRight(prompt.String(), "\n"))})
	if err != nil {
		return fmt.Errorf("acp history replay: %w", err)
	}
	return nil
}

func messageText(msg agentkit.ModelMessage) string {
	var b strings.Builder
	for _, part := range msg.Content {
		if part.Type == "" || part.Type == "text" {
			b.WriteString(part.Text)
		}
	}
	return b.String()
}
