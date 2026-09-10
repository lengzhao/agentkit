package acpremote

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	acp "github.com/coder/acp-go-sdk"
	"github.com/lengzhao/agentkit"
	rtsession "github.com/lengzhao/agentkit/runtime/session"
)

const defaultSessionDir = "sessions"
const maxReplayMessages = 40

type acpSessionBind struct {
	AgentID      agentkit.AgentID `json:"agentId"`
	ACPSessionID acp.SessionId    `json:"acpSessionId"`
	Cwd          string           `json:"cwd"`
	// McpFingerprint matches runtime/acpclient.Fingerprint of harness MCP servers at session/new.
	McpFingerprint string `json:"mcpFingerprint,omitempty"`
}

func acpSessionBindPath(storeDir string, sessionID agentkit.SessionID, agentID agentkit.AgentID) (string, error) {
	workDir, err := rtsession.WorkDir(storeDir, sessionID)
	if err != nil {
		return "", err
	}
	name, err := safeAgentIDForFile(agentID)
	if err != nil {
		return "", err
	}
	return filepath.Join(workDir, "acp-session."+name+".json"), nil
}

func safeAgentIDForFile(id agentkit.AgentID) (string, error) {
	raw := string(id)
	if raw == "" {
		return "", fmt.Errorf("empty agent id")
	}
	if strings.Contains(raw, "..") {
		return "", fmt.Errorf("invalid agent id")
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

func loadACPSessionBind(path string) (acpSessionBind, bool, error) {
	data, err := os.ReadFile(path)
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

func saveACPSessionBind(path string, bind acpSessionBind) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	raw, err := json.Marshal(bind)
	if err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0o644)
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
