package send

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/lengzhao/agentkit"
	capsdelivery "github.com/lengzhao/agentkit/cap/delivery"
	rtdelivery "github.com/lengzhao/agentkit/runtime/delivery"
	"github.com/lengzhao/agentkit/cap/workspace"
)

// Dispatch sends a proactive message through the delivery sender.
func Dispatch(ctx context.Context, deps SendDeps, cfg SendConfig, input SendInput) error {
	root := strings.TrimSpace(cfg.Root)
	if root == "" {
		root = "."
	}
	if deps.Sender == nil {
		return fmt.Errorf("tool/send requires sender dependency")
	}
	parts, err := buildParts(ctx, input, deps.Workspace, root)
	if err != nil {
		return err
	}
	route, err := rtdelivery.ResolveRoute(ctx, capsdelivery.RouteInput{
		SessionID: input.SessionID,
		UserID:    input.UserID,
	})
	if err != nil {
		return err
	}
	modelMsg := agentkit.ModelMessage{Role: "assistant", Content: parts}
	event := agentkit.OutboundEvent{
		Route:      rtdelivery.OutboundRoute(route.PlatformID, route.SessionID),
		AgentID:    route.AgentID,
		PlatformID: route.PlatformID,
		UserID:     route.UserID,
		Type:       agentkit.EventAssistantMessage,
		Data:       agentkit.MarshalOutboundData(modelMsg),
	}
	if err := event.RequirePlatformID(); err != nil {
		return err
	}
	if useEmit(ctx, input) {
		if emit := agentkit.OutboundEmitFromContext(ctx); emit != nil {
			ctx = context.WithValue(ctx, agentkit.KeyProactiveSendUsed, true)
			return emit(ctx, event)
		}
	}
	return deps.Sender.Send(ctx, event)
}

// ParseSlashArgs parses /send arguments.
//   - /send <sessionId> <message>      → explicit session (contains ":" or Slack channel id)
//   - /send @<userId> <message>        → user in current channel context
func ParseSlashArgs(args string) (SendInput, error) {
	args = strings.TrimSpace(args)
	if args == "" {
		return SendInput{}, fmt.Errorf("usage: /send <sessionId> <message>\n       /send @<userId> <message>")
	}
	firstSpace := strings.IndexByte(args, ' ')
	if firstSpace < 0 {
		if isSlashTarget(args) {
			return SendInput{}, fmt.Errorf("message is required")
		}
		return SendInput{}, fmt.Errorf("usage: /send <sessionId> <message>\n       /send @<userId> <message>")
	}
	first := args[:firstSpace]
	if !isSlashTarget(first) {
		return SendInput{}, fmt.Errorf("usage: /send <sessionId> <message>\n       /send @<userId> <message>")
	}
	message := strings.TrimSpace(args[firstSpace+1:])
	if message == "" {
		return SendInput{}, fmt.Errorf("message is required")
	}
	input := SendInput{Text: message}
	if strings.HasPrefix(first, "@") {
		input.UserID = strings.TrimPrefix(first, "@")
	} else {
		input.SessionID = first
	}
	return input, nil
}

func isSlashTarget(token string) bool {
	if strings.HasPrefix(token, "@") && len(token) > 1 {
		return true
	}
	if strings.Contains(token, ":") {
		return true
	}
	return rtdelivery.IsSlackChannelID(token)
}

func buildParts(ctx context.Context, input SendInput, ws workspace.Service, root string) ([]agentkit.ContentPart, error) {
	text := strings.TrimSpace(input.Text)
	path := strings.TrimSpace(input.Path)
	if text == "" && path == "" {
		return nil, fmt.Errorf("send requires text or path")
	}
	var parts []agentkit.ContentPart
	if text != "" {
		parts = append(parts, agentkit.ContentPart{Type: contentText, Text: text})
	}
	if path != "" {
		if ws == nil {
			return nil, fmt.Errorf("path %q requires workspace dependency", path)
		}
		rel := path
		if root != "." {
			rel = filepath.Join(root, path)
		}
		url, err := ws.Resolve(ctx, rel)
		if err != nil {
			return nil, err
		}
		if _, err := os.Stat(url); err != nil {
			return nil, fmt.Errorf("file not found: %s", path)
		}
		if isImagePath(path) {
			parts = append(parts, agentkit.ContentPart{Type: contentImage, URL: url})
		} else {
			parts = append(parts, agentkit.ContentPart{Type: contentDocument, URL: url})
		}
	}
	return parts, nil
}

func isImagePath(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".bmp", ".svg":
		return true
	default:
		return false
	}
}
