package acpplatform

import (
	"testing"

	acp "github.com/coder/acp-go-sdk"
	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/permission"
)

func TestPromptToModelMessage(t *testing.T) {
	msg := promptToModelMessage([]acp.ContentBlock{
		acp.TextBlock("hello"),
		acp.ResourceLinkBlock("image", "https://example.com/a.png"),
	})
	if msg.Role != "user" {
		t.Fatalf("role = %q", msg.Role)
	}
	if len(msg.Content) != 2 {
		t.Fatalf("content len = %d, want 2", len(msg.Content))
	}
	if msg.Content[0].Text != "hello" {
		t.Fatalf("text = %q", msg.Content[0].Text)
	}
	if msg.Content[1].URL != "https://example.com/a.png" {
		t.Fatalf("url = %q", msg.Content[1].URL)
	}
}

func TestPermissionToACPAllowDeny(t *testing.T) {
	req := permissionToACP(acp.SessionId("sess_1"), permission.RequestPayload{
		Request: permission.Request{
			ID:   "perm1",
			Kind: permission.KindAllowDeny,
			ToolCall: &agentkit.ToolCall{
				ID:   "call_1",
				Name: "bash",
				Input: []byte(`{"cmd":"ls"}`),
			},
		},
	})
	if req.SessionId != "sess_1" {
		t.Fatalf("session = %q", req.SessionId)
	}
	if string(req.ToolCall.ToolCallId) != "call_1" {
		t.Fatalf("tool id = %q", req.ToolCall.ToolCallId)
	}
	if len(req.Options) != 2 {
		t.Fatalf("options = %d", len(req.Options))
	}
}

func TestACPPermissionToReply(t *testing.T) {
	reply := acpPermissionToReply("perm1", acp.RequestPermissionResponse{
		Outcome: acp.RequestPermissionOutcome{
			Selected: &acp.RequestPermissionOutcomeSelected{OptionId: acp.PermissionOptionId("allow")},
		},
	})
	if reply.RequestID != "perm1" || reply.Decision != "allow" {
		t.Fatalf("reply = %+v", reply)
	}

	reply = acpPermissionToReply("perm2", acp.RequestPermissionResponse{
		Outcome: acp.RequestPermissionOutcome{
			Selected: &acp.RequestPermissionOutcomeSelected{OptionId: acp.PermissionOptionId("2")},
		},
	})
	if len(reply.Selected) != 1 || reply.Selected[0] != 1 {
		t.Fatalf("selected = %v", reply.Selected)
	}
}

func TestPlatformID(t *testing.T) {
	plat, err := New(Config{}, Deps{})
	if err != nil {
		t.Fatal(err)
	}
	p, ok := plat.(*Platform)
	if !ok {
		t.Fatal("expected *Platform")
	}
	if p.PlatformID() != platformID {
		t.Fatalf("id = %q", p.PlatformID())
	}
	cap := p.PermissionCapability()
	if !cap.Interactive {
		t.Fatalf("cap = %+v", cap)
	}
}
