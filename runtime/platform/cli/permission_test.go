package cli

import (
	"context"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/permission"
	rtpermission "github.com/lengzhao/agentkit/runtime/permission"
)

func TestCLIPermissionRequestReply(t *testing.T) {
	t.Parallel()

	p, err := New(Config{}, Deps{})
	if err != nil {
		t.Fatal(err)
	}
	platform := p.(*Platform)
	platform.input = NewInput(strings.NewReader("sqlite\n"))

	payload := agentkit.MarshalOutboundData(permission.RequestPayload{
		Request: permission.Request{
			ID:   "perm1",
			Kind: permission.KindQuestion,
			Question: &permission.Question{
				Prompt:  "which store?",
				Options: []permission.Option{{Label: "jsonl"}, {Label: "sqlite"}},
				Default: "jsonl",
			},
		},
	})
	if err := platform.Send(context.Background(), agentkit.OutboundEvent{
		Type: agentkit.EventPermissionRequest,
		Data: payload,
	}); err != nil {
		t.Fatal(err)
	}

	event, err := platform.Receive(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(event.Reply) == 0 {
		t.Fatal("expected permission reply event")
	}
	reply, err := rtpermission.DecodeReply(event.Reply)
	if err != nil {
		t.Fatal(err)
	}
	if reply.RequestID != "perm1" || reply.Text != "sqlite" {
		t.Fatalf("reply = %+v", reply)
	}
}

func TestCLIApprovalRequestReply(t *testing.T) {
	t.Parallel()

	p, err := New(Config{}, Deps{})
	if err != nil {
		t.Fatal(err)
	}
	platform := p.(*Platform)
	platform.input = NewInput(strings.NewReader("y\n"))

	payload := agentkit.MarshalOutboundData(permission.RequestPayload{
		Request: permission.Request{
			ID:     "perm2",
			Kind:   permission.KindAllowDeny,
			Reason: "dangerous shell",
			ToolCall: &agentkit.ToolCall{
				ID:   "call-1",
				Name: "bash",
			},
		},
	})
	if err := platform.Send(context.Background(), agentkit.OutboundEvent{
		Type: agentkit.EventPermissionRequest,
		Data: payload,
	}); err != nil {
		t.Fatal(err)
	}

	event, err := platform.Receive(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(event.Reply) == 0 {
		t.Fatal("expected permission reply event")
	}
	reply, err := rtpermission.DecodeReply(event.Reply)
	if err != nil {
		t.Fatal(err)
	}
	if reply.Text != "y" {
		t.Fatalf("reply = %+v", reply)
	}
}

func TestCLIPermissionCapability(t *testing.T) {
	t.Parallel()

	p, err := New(Config{}, Deps{})
	if err != nil {
		t.Fatal(err)
	}
	cap := p.(permission.Capable).PermissionCapability()
	if !cap.Interactive || cap.AnswerScope != permission.ScopeAnyone {
		t.Fatalf("cap = %+v", cap)
	}
	if cap.DefaultTimeout != permission.DefaultTimeout {
		t.Fatalf("DefaultTimeout = %v, want %v", cap.DefaultTimeout, permission.DefaultTimeout)
	}
}

// TestReceiveRoutesBlockedPromptToPermission covers the case where Receive is
// already blocked on the main prompt when a permission request arrives on Send.
func TestReceiveRoutesBlockedPromptToPermission(t *testing.T) {
	t.Parallel()

	pr, pw := io.Pipe()
	platform := &Platform{
		input:     NewInput(pr),
		sessionID: "cli:default",
		welcomed:  true,
	}

	payload := agentkit.MarshalOutboundData(permission.RequestPayload{
		Request: permission.Request{
			ID:     "perm3",
			Kind:   permission.KindAllowDeny,
			Reason: "dangerous shell",
			ToolCall: &agentkit.ToolCall{
				ID:   "call-1",
				Name: "bash",
			},
		},
	})

	done := make(chan agentkit.MessageEvent, 1)
	errCh := make(chan error, 1)
	go func() {
		event, err := platform.Receive(context.Background())
		if err != nil {
			errCh <- err
			return
		}
		done <- event
	}()

	// Let Receive block on the main prompt before the turn emits permission.
	time.Sleep(20 * time.Millisecond)

	if err := platform.Send(context.Background(), agentkit.OutboundEvent{
		Type: agentkit.EventPermissionRequest,
		Data: payload,
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := pw.Write([]byte("y\n")); err != nil {
		t.Fatal(err)
	}

	select {
	case err := <-errCh:
		t.Fatalf("receive: %v", err)
	case event := <-done:
		reply, err := rtpermission.DecodeReply(event.Reply)
		if err != nil {
			t.Fatal(err)
		}
		if reply.RequestID != "perm3" || reply.Text != "y" {
			t.Fatalf("reply = %+v", reply)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for permission reply")
	}
}

func TestSendReceivePendingConcurrent(t *testing.T) {
	t.Parallel()

	pr, pw := io.Pipe()
	platform := &Platform{
		input:     NewInput(pr),
		sessionID: "cli:default",
		welcomed:  true,
	}

	payload := agentkit.MarshalOutboundData(permission.RequestPayload{
		Request: permission.Request{
			ID:   "perm4",
			Kind: permission.KindQuestion,
			Question: &permission.Question{
				Prompt: "pick",
			},
		},
	})

	done := make(chan agentkit.MessageEvent, 1)
	go func() {
		event, err := platform.Receive(context.Background())
		if err != nil {
			t.Error(err)
			return
		}
		done <- event
	}()

	time.Sleep(20 * time.Millisecond)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := platform.Send(context.Background(), agentkit.OutboundEvent{
			Type: agentkit.EventPermissionRequest,
			Data: payload,
		}); err != nil {
			t.Error(err)
		}
	}()
	wg.Wait()

	if _, err := pw.Write([]byte("beta\n")); err != nil {
		t.Fatal(err)
	}

	select {
	case event := <-done:
		if len(event.Reply) == 0 {
			t.Fatalf("event = %+v", event)
		}
		reply, err := rtpermission.DecodeReply(event.Reply)
		if err != nil {
			t.Fatal(err)
		}
		if reply.Text != "beta" {
			t.Fatalf("reply = %+v", reply)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for permission reply")
	}
}
