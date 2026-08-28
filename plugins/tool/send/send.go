package send

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/workspace"
	"github.com/lengzhao/agentkit/runtime/session"
)

const (
	contentText     = "text"
	contentImage    = "image"
	contentDocument = "document"
)

type SendConfig struct{}

type SendDeps struct {
	Platform  agentkit.Platform `json:"platform"`
	Workspace workspace.Service `json:"workspace,omitempty"`
}

type SendInput struct {
	Text      string `json:"text,omitempty" jsonschema:"Text message to send"`
	Path      string `json:"path,omitempty" jsonschema:"Workspace-relative file to send (image or document)"`
	SessionID string `json:"sessionId,omitempty" jsonschema:"Optional delivery target; defaults to the current inbox"`
	UserID    string `json:"userId,omitempty" jsonschema:"Optional user target when sessionId is omitted"`
}

type SendOutput struct {
	Sent bool `json:"sent"`
}

// NewSend registers tool/send: Send a proactive user-visible message through the platform.
//
// Best practices:
//   - Wire the same platform instance runner uses (platform.default).
//   - Text and path may be sent together (text first, then file). Path needs the workspace dep.
//   - Platform/channel routing comes from context. Target sessionId, userId, or neither (current inbox).
func NewSend(_ SendConfig, deps SendDeps) (agentkit.Tool, error) {
	if deps.Platform == nil {
		return nil, fmt.Errorf("tool/send requires platform dependency")
	}
	platform := deps.Platform
	tool, err := agentkit.NewTool[SendInput, SendOutput]("send", func(ctx context.Context, input SendInput) (SendOutput, error) {
		parts, err := buildParts(ctx, input, deps.Workspace)
		if err != nil {
			return SendOutput{}, err
		}
		route, err := resolveRoute(ctx, input)
		if err != nil {
			return SendOutput{}, err
		}
		modelMsg := agentkit.ModelMessage{Role: "assistant", Content: parts}
		event := agentkit.OutboundEvent{
			SessionID:  route.sessionID,
			AgentID:    route.agentID,
			PlatformID: route.platformID,
			UserID:     route.userID,
			Type:       agentkit.EventAssistantMessage,
			Data:       agentkit.MarshalOutboundData(modelMsg),
		}
		if useEmit(ctx, input) {
			if emit, ok := ctx.Value(agentkit.KeyOutboundEmit).(agentkit.OutboundEmit); ok && emit != nil {
				if err := emit(ctx, event); err != nil {
					return SendOutput{}, err
				}
				return SendOutput{Sent: true}, nil
			}
		}
		if err := platform.Send(ctx, event); err != nil {
			return SendOutput{}, err
		}
		return SendOutput{Sent: true}, nil
	}).
		Description("Send a proactive message to the user now: text, a workspace file, or both. Use for progress updates that should not wait until the turn ends.").
		Build()
	if err != nil {
		return nil, err
	}
	return tool, nil
}

type route struct {
	sessionID  agentkit.SessionID
	agentID    agentkit.AgentID
	platformID string
	userID     string
}

func useEmit(_ context.Context, input SendInput) bool {
	return strings.TrimSpace(input.SessionID) == "" && strings.TrimSpace(input.UserID) == ""
}

func resolveRoute(ctx context.Context, input SendInput) (route, error) {
	inbox, _ := ctx.Value(agentkit.KeyDeliverySessionID).(agentkit.SessionID)
	if inbox == "" {
		inbox, _ = ctx.Value(agentkit.KeySessionID).(agentkit.SessionID)
	}

	var r route
	switch {
	case strings.TrimSpace(input.SessionID) != "":
		r.sessionID = agentkit.SessionID(strings.TrimSpace(input.SessionID))
	case strings.TrimSpace(input.UserID) != "":
		r.sessionID = session.DeliveryWithUser(inbox, input.UserID)
	default:
		r.sessionID = inbox
	}
	if r.sessionID == "" {
		return route{}, fmt.Errorf("send requires inbox session in context, or sessionId/userId")
	}
	r.agentID, _ = ctx.Value(agentkit.KeyAgentID).(agentkit.AgentID)
	r.platformID, _ = ctx.Value(agentkit.KeyPlatformID).(string)
	if id := strings.TrimSpace(input.UserID); id != "" {
		r.userID = id
	} else {
		r.userID, _ = ctx.Value(agentkit.KeyUserID).(string)
	}
	return r, nil
}

func buildParts(ctx context.Context, input SendInput, ws workspace.Service) ([]agentkit.ContentPart, error) {
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
		url, err := ws.Resolve(ctx, path)
		if err != nil {
			return nil, err
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
