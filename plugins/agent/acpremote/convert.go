package acpremote

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	acp "github.com/coder/acp-go-sdk"
	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/permission"
	rtpermission "github.com/lengzhao/agentkit/runtime/permission"
)

func modelMessageToPrompt(msg agentkit.ModelMessage) []acp.ContentBlock {
	var blocks []acp.ContentBlock
	for _, part := range msg.Content {
		switch part.Type {
		case "", "text":
			if part.Text != "" {
				blocks = append(blocks, acp.TextBlock(part.Text))
			}
		case "image":
			if part.URL != "" {
				blocks = append(blocks, acp.ResourceLinkBlock("image", part.URL))
			}
		}
	}
	return blocks
}

func readTextFile(path string, line, limit *int) (string, error) {
	if !filepath.IsAbs(path) {
		return "", fmt.Errorf("path must be absolute: %s", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	content := string(data)
	if line == nil && limit == nil {
		return content, nil
	}
	lines := strings.Split(content, "\n")
	start := 0
	if line != nil && *line > 0 {
		start = min(max(*line-1, 0), len(lines))
	}
	end := len(lines)
	if limit != nil && *limit > 0 && start+*limit < end {
		end = start + *limit
	}
	return strings.Join(lines[start:end], "\n"), nil
}

func writeTextFile(path, content string) error {
	if !filepath.IsAbs(path) {
		return fmt.Errorf("path must be absolute: %s", path)
	}
	dir := filepath.Dir(path)
	if dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	return os.WriteFile(path, []byte(content), 0o644)
}

func autoApprovePermission(params acp.RequestPermissionRequest) acp.RequestPermissionResponse {
	for _, o := range params.Options {
		if o.Kind == acp.PermissionOptionKindAllowOnce || o.Kind == acp.PermissionOptionKindAllowAlways {
			return acp.RequestPermissionResponse{
				Outcome: acp.RequestPermissionOutcome{
					Selected: &acp.RequestPermissionOutcomeSelected{OptionId: o.OptionId},
				},
			}
		}
	}
	if len(params.Options) > 0 {
		return acp.RequestPermissionResponse{
			Outcome: acp.RequestPermissionOutcome{
				Selected: &acp.RequestPermissionOutcomeSelected{OptionId: params.Options[0].OptionId},
			},
		}
	}
	return denyPermission(params)
}

func denyPermission(params acp.RequestPermissionRequest) acp.RequestPermissionResponse {
	_ = params
	return acp.RequestPermissionResponse{
		Outcome: acp.RequestPermissionOutcome{
			Cancelled: &acp.RequestPermissionOutcomeCancelled{},
		},
	}
}

func requestPermissionViaBroker(ctx context.Context, params acp.RequestPermissionRequest) (acp.RequestPermissionResponse, error) {
	broker, ok := rtpermission.BrokerFrom(ctx)
	if !ok {
		return denyPermission(params), nil
	}
	title := ""
	if params.ToolCall.Title != nil {
		title = *params.ToolCall.Title
	}
	toolCall := &agentkit.ToolCall{
		ID:   agentkit.ToolCallID(params.ToolCall.ToolCallId),
		Name: title,
	}
	if params.ToolCall.RawInput != nil {
		if b, err := json.Marshal(params.ToolCall.RawInput); err == nil {
			toolCall.Input = b
		}
	}
	result, err := broker.Await(ctx, permission.Request{
		Kind:     permission.KindAllowDeny,
		Reason:   title,
		ToolCall: toolCall,
	})
	if err != nil {
		return acp.RequestPermissionResponse{}, err
	}
	if !result.Allow {
		return denyPermission(params), nil
	}
	for _, o := range params.Options {
		if o.Kind == acp.PermissionOptionKindAllowOnce || o.Kind == acp.PermissionOptionKindAllowAlways {
			return acp.RequestPermissionResponse{
				Outcome: acp.RequestPermissionOutcome{
					Selected: &acp.RequestPermissionOutcomeSelected{OptionId: o.OptionId},
				},
			}, nil
		}
	}
	if len(params.Options) > 0 {
		return acp.RequestPermissionResponse{
			Outcome: acp.RequestPermissionOutcome{
				Selected: &acp.RequestPermissionOutcomeSelected{OptionId: params.Options[0].OptionId},
			},
		}, nil
	}
	return denyPermission(params), nil
}
