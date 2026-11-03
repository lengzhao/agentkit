package send

import (
	"context"
	"errors"
	"fmt"

	"github.com/lengzhao/agentkit"
	capsdelivery "github.com/lengzhao/agentkit/cap/delivery"
	"github.com/lengzhao/agentkit/cap/filesystem"
	"github.com/lengzhao/agentkit/cap/workspace"
)

const (
	contentText     = "text"
	contentImage    = "image"
	contentDocument = "document"
)

type SendConfig struct {
	// Root is the workspace subdirectory files are resolved from, matching
	// tool/fs-workspace root (typically ".").
	Root string `json:"root"`
}

type SendDeps struct {
	Messages  capsdelivery.Publisher `json:"messages,omitempty"`
	Sender    capsdelivery.Sender    `json:"sender"`
	Delivery  capsdelivery.Assistant `json:"delivery"`
	Workspace workspace.Service      `json:"workspace,omitempty"`
	// FS verifies attachment existence (wire an unrestricted filesystem/local
	// instance; the resolved path is also handed to the platform as a local URL).
	FS filesystem.Service `json:"fs,omitempty"`
}

type SendInput struct {
	Receipt     bool               `json:"receipt,omitempty" jsonschema:"Request durable platform message references; requires messages dependency"`
	OperationID string             `json:"operationId,omitempty" jsonschema:"Stable operation ID for receipt-mode retries of identical content"`
	Route       *agentkit.RouteRef `json:"route,omitempty" jsonschema:"Explicit target for receipt delivery, including platform instance; works without a turn"`
	Text        string             `json:"text,omitempty" jsonschema:"Text message to send"`
	Path        string             `json:"path,omitempty" jsonschema:"Agent work-tree file to send (same paths as read/write, e.g. openapi/foo.json or work/foo.json; image or document)"`
	SessionID   string             `json:"sessionId,omitempty" jsonschema:"Optional delivery target; defaults to the current inbox"`
	UserID      string             `json:"userId,omitempty" jsonschema:"Optional user target when sessionId is omitted"`
	Raw         bool               `json:"raw,omitempty" jsonschema:"Send plain text without platform markdown conversion"`
}

type SendOutput struct {
	Sent      bool                   `json:"sent"`
	Receipt   *capsdelivery.Receipt  `json:"receipt,omitempty"`
	Error     string                 `json:"error,omitempty"`
	ErrorCode capsdelivery.ErrorCode `json:"errorCode,omitempty"`
}

// NewSend registers tool/send: Send a proactive user-visible message through the platform.
//
// Best practices:
//   - Wire the same sender instance runner uses (platform.default).
//   - Text and path may be sent together (text first, then file). Path needs the workspace dep.
//   - Platform/channel routing comes from context. Target sessionId, userId, or neither (current inbox).
//   - Slash: /send [-r|--raw] <chatId> <message> (bare channel/chat id; platform from context; message may be multiline)
func NewSend(cfg SendConfig, deps SendDeps) (agentkit.Tool, error) {
	if deps.Sender == nil {
		return nil, fmt.Errorf("tool/send requires sender dependency")
	}
	if deps.Delivery == nil {
		return nil, fmt.Errorf("tool/send requires delivery dependency")
	}
	tool, err := agentkit.NewTool[SendInput, SendOutput]("send", func(ctx context.Context, input SendInput) (SendOutput, error) {
		if input.Receipt || input.Route != nil || input.OperationID != "" {
			receipt, err := DispatchReceipt(ctx, deps, input)
			out := SendOutput{Sent: err == nil, Receipt: &receipt}
			if err != nil {
				out.Error = err.Error()
				var e *capsdelivery.Error
				if errors.As(err, &e) {
					out.ErrorCode = e.Code
				}
			}
			// Preserve partial/unknown receipts in structured tool output. The generic
			// ToolBuilder converts handler errors to text and discards typed results.
			return out, nil
		}
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
	// RawMessage represents a JSON object here, not a byte-array tool argument.
	schema := tool.InputSchema()
	route := schema.Properties["route"]
	route.Properties["target"] = agentkit.JSONSchema{Type: "object", Properties: map[string]agentkit.JSONSchema{
		"channelId": {Type: "string"}, "threadId": {Type: "string"}, "deliveryId": {Type: "string"}, "scopeUserId": {Type: "string"}, "replyTo": {Type: "string"},
	}}
	schema.Properties["route"] = route
	return &sendBundle{tool: tool, cfg: cfg, deps: deps, schema: schema}, nil
}
