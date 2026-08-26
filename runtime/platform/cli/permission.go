package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/permission"
)

type permissionPrompt struct {
	requestID string
}

func (p *Platform) PermissionCapability() permission.Capability {
	return permission.Capability{
		Interactive:    true,
		DefaultTimeout: permission.DefaultTimeout,
		AnswerScope:    permission.ScopeAnyone,
	}
}

func (p *Platform) renderPermissionRequest(payload permission.RequestPayload) {
	switch payload.Kind {
	case permission.KindAllowDeny:
		tool := ""
		if payload.ToolCall != nil {
			tool = payload.ToolCall.Name
		}
		reason := strings.TrimSpace(payload.Reason)
		if reason == "" {
			reason = "approval required"
		}
		fmt.Fprintf(os.Stderr, "\n[approval needed] tool %q: %s [y/N] ", tool, reason)
	case permission.KindQuestion:
		if payload.Question == nil {
			return
		}
		q := *payload.Question
		header := "[agent asks]"
		if h := strings.TrimSpace(q.Header); h != "" {
			header = h
		}
		fmt.Fprintf(os.Stderr, "\n%s %s\n", header, q.Prompt)
		for i, opt := range q.Options {
			label := strings.TrimSpace(opt.Label)
			if label == "" {
				continue
			}
			fmt.Fprintf(os.Stderr, "  %d) %s\n", i+1, label)
		}
		if q.Default != "" {
			fmt.Fprintf(os.Stderr, "answer [%s]: ", q.Default)
		} else {
			fmt.Fprint(os.Stderr, "answer: ")
		}
	}
}

func decodePermissionRequest(data json.RawMessage) (permission.RequestPayload, error) {
	var payload permission.RequestPayload
	if err := json.Unmarshal(data, &payload); err != nil {
		return permission.RequestPayload{}, err
	}
	return payload, nil
}

func (p *Platform) permissionReplyEvent(text string, pending *permissionPrompt) agentkit.MessageEvent {
	return agentkit.MessageEvent{
		SessionID:  p.sessionID,
		PlatformID: "cli",
		Reply: permission.MarshalReply(permission.Reply{
			RequestID: pending.requestID,
			Text:      text,
		}),
	}
}

func (p *Platform) hasPending() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.pending != nil
}

func (p *Platform) takePending() *permissionPrompt {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.pending == nil {
		return nil
	}
	pending := p.pending
	p.pending = nil
	return pending
}
