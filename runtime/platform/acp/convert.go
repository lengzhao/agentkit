package acpplatform

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	acp "github.com/coder/acp-go-sdk"
	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/permission"
)

func promptToModelMessage(blocks []acp.ContentBlock) agentkit.ModelMessage {
	msg := agentkit.ModelMessage{Role: "user"}
	for _, block := range blocks {
		if block.Text != nil && block.Text.Text != "" {
			msg.Content = append(msg.Content, agentkit.ContentPart{
				Type: "text",
				Text: block.Text.Text,
			})
		}
		if block.ResourceLink != nil && block.ResourceLink.Uri != "" {
			msg.Content = append(msg.Content, agentkit.ContentPart{
				Type: "image",
				URL:  block.ResourceLink.Uri,
			})
		}
	}
	return msg
}

func permissionToACP(sessionID acp.SessionId, payload permission.RequestPayload) acp.RequestPermissionRequest {
	title := strings.TrimSpace(payload.Reason)
	req := acp.RequestPermissionRequest{
		SessionId: sessionID,
		ToolCall: acp.ToolCallUpdate{
			Title: acp.Ptr(title),
		},
	}
	switch payload.Kind {
	case permission.KindAllowDeny:
		if payload.ToolCall != nil {
			req.ToolCall.ToolCallId = acp.ToolCallId(payload.ToolCall.ID)
			if name := strings.TrimSpace(payload.ToolCall.Name); name != "" {
				req.ToolCall.Title = acp.Ptr(name)
			}
			if len(payload.ToolCall.Input) > 0 {
				var raw map[string]any
				if err := json.Unmarshal(payload.ToolCall.Input, &raw); err == nil {
					req.ToolCall.RawInput = raw
				}
			}
		}
		req.Options = []acp.PermissionOption{
			{Kind: acp.PermissionOptionKindAllowOnce, Name: "Allow", OptionId: acp.PermissionOptionId("allow")},
			{Kind: acp.PermissionOptionKindRejectOnce, Name: "Deny", OptionId: acp.PermissionOptionId("deny")},
		}
	case permission.KindQuestion:
		if payload.Question != nil {
			q := payload.Question
			if title == "" {
				title = strings.TrimSpace(q.Prompt)
			}
			req.ToolCall.Title = acp.Ptr(title)
			for i, opt := range q.Options {
				label := strings.TrimSpace(opt.Label)
				if label == "" {
					continue
				}
				req.Options = append(req.Options, acp.PermissionOption{
					Kind:     acp.PermissionOptionKindAllowOnce,
					Name:     label,
					OptionId: acp.PermissionOptionId(fmt.Sprintf("%d", i+1)),
				})
			}
		}
	}
	return req
}

func acpPermissionToReply(requestID string, resp acp.RequestPermissionResponse) permission.Reply {
	reply := permission.Reply{RequestID: requestID}
	if resp.Outcome.Cancelled != nil {
		reply.Cancelled = true
		return reply
	}
	if resp.Outcome.Selected == nil {
		reply.Cancelled = true
		return reply
	}
	optionID := string(resp.Outcome.Selected.OptionId)
	switch optionID {
	case "allow":
		reply.Decision = "allow"
	case "deny":
		reply.Decision = "deny"
		reply.Cancelled = true
	default:
		if n, err := strconv.Atoi(optionID); err == nil && n > 0 {
			reply.Selected = []int{n - 1}
		} else {
			reply.Text = optionID
		}
	}
	return reply
}
