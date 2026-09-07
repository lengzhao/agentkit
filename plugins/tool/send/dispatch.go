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
	if input.Raw {
		ctx = context.WithValue(ctx, agentkit.KeyProactiveSendRaw, true)
	}
	if useEmit(ctx, input) {
		if emit := agentkit.OutboundEmitFromContext(ctx); emit != nil {
			ctx = context.WithValue(ctx, agentkit.KeyProactiveSendUsed, true)
			return emit(ctx, event)
		}
	}
	return deps.Sender.Send(ctx, event)
}

// ParseSlashArgs parses /send arguments: /send [-r|--raw] <chatId> <message>.
// chatId is a bare channel/chat id for the current platform from context.
// Message may span multiple lines when the platform passes them in one payload.
func ParseSlashArgs(args string) (SendInput, error) {
	args = strings.TrimSpace(args)
	raw := false
	for {
		switch {
		case strings.HasPrefix(args, "--raw "):
			raw = true
			args = strings.TrimSpace(args[len("--raw "):])
		case args == "--raw":
			return SendInput{}, usageError()
		case strings.HasPrefix(args, "-r "):
			raw = true
			args = strings.TrimSpace(args[3:])
		case args == "-r":
			return SendInput{}, usageError()
		default:
			goto parsedFlags
		}
	}
parsedFlags:
	if args == "" {
		return SendInput{}, usageError()
	}
	firstSpace := strings.IndexByte(args, ' ')
	if firstSpace < 0 {
		if isChatID(args) {
			return SendInput{}, fmt.Errorf("message is required")
		}
		return SendInput{}, usageError()
	}
	chatID := args[:firstSpace]
	if !isChatID(chatID) {
		return SendInput{}, usageError()
	}
	message := strings.TrimSpace(args[firstSpace+1:])
	if message == "" {
		return SendInput{}, fmt.Errorf("message is required")
	}
	return SendInput{Text: message, SessionID: chatID, Raw: raw}, nil
}

func usageError() error {
	return fmt.Errorf("usage: /send [-r|--raw] <chatId> <message>")
}

func isChatID(token string) bool {
	token = strings.TrimSpace(token)
	if token == "" || strings.Contains(token, ":") || strings.HasPrefix(token, "@") {
		return false
	}
	hasUpper, hasDigit, hasUnderscore := false, false, false
	for _, r := range token {
		switch {
		case r == '_':
			hasUnderscore = true
		case r >= 'A' && r <= 'Z':
			hasUpper = true
		case r >= '0' && r <= '9':
			hasDigit = true
		}
	}
	return hasUnderscore || (hasUpper && hasDigit)
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
