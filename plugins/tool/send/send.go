package send

import (
	"context"
	"fmt"
	"strings"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/workspace"
)

const (
	contentText     = "text"
	contentImage    = "image"
	contentDocument = "document"
)

type SendConfig struct {
	// Root is the workspace subdirectory files are resolved from, matching
	// tool/fs-workspace root (typically "work").
	Root string `json:"root"`
}

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

type route struct {
	sessionID  agentkit.SessionID
	agentID    agentkit.AgentID
	platformID string
	userID     string
}

func useEmit(_ context.Context, input SendInput) bool {
	return strings.TrimSpace(input.SessionID) == "" && strings.TrimSpace(input.UserID) == ""
}

// NewSend registers tool/send: Send a proactive user-visible message through the platform.
//
// Best practices:
//   - Wire the same platform instance runner uses (platform.default).
//   - Text and path may be sent together (text first, then file). Path needs the workspace dep.
//   - Platform/channel routing comes from context. Target sessionId, userId, or neither (current inbox).
//   - Slash: /send <message> | /send <sessionId> <message> | /send @<userId> <message>
func NewSend(cfg SendConfig, deps SendDeps) (agentkit.Tool, error) {
	if deps.Platform == nil {
		return nil, fmt.Errorf("tool/send requires platform dependency")
	}
	tool, err := agentkit.NewTool[SendInput, SendOutput]("send", func(ctx context.Context, input SendInput) (SendOutput, error) {
		if err := Dispatch(ctx, deps, cfg, input); err != nil {
			return SendOutput{}, err
		}
		return SendOutput{Sent: true}, nil
	}).
		Description("Send a proactive message to the user now: text, a workspace file, or both. Use for progress updates that should not wait until the turn ends.").
		Build()
	if err != nil {
		return nil, err
	}
	return &sendBundle{tool: tool, cfg: cfg, deps: deps}, nil
}
