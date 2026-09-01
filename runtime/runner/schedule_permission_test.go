package runner

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/permission"
)

type stubCapablePlatform struct {
	id  string
	cap permission.Capability
}

func (p *stubCapablePlatform) PlatformID() string { return p.id }

func (p *stubCapablePlatform) Receive(context.Context) (agentkit.MessageEvent, error) {
	return agentkit.MessageEvent{}, io.EOF
}

func (p *stubCapablePlatform) Send(context.Context, agentkit.OutboundEvent) error { return nil }

func (p *stubCapablePlatform) PermissionCapability() permission.Capability { return p.cap }

type stubCapabilityRouter struct {
	stubCapablePlatform
	leaves map[string]permission.Capability
}

func (p *stubCapabilityRouter) PermissionCapabilityFor(id string) permission.Capability {
	if cap, ok := p.leaves[id]; ok {
		return cap
	}
	return permission.Capability{Interactive: false}
}

func scheduleFireMetadata() map[string]any {
	return map[string]any{
		"schedule": map[string]any{
			"fired":       true,
			"jobId":       "agent-1",
			"kind":        "delay",
			"sessionMode": "stateless",
		},
	}
}

func TestInboundPermissionCapabilityScheduleFireNonInteractive(t *testing.T) {
	interactive := permission.Capability{
		Interactive:    true,
		DefaultTimeout: 5 * time.Minute,
		AnswerScope:    permission.ScopeAsker,
	}
	router := &stubCapabilityRouter{
		stubCapablePlatform: stubCapablePlatform{id: "multiplex", cap: permission.Capability{Interactive: false}},
		leaves: map[string]permission.Capability{
			"chat-api": interactive,
		},
	}

	event := agentkit.MessageEvent{
		PlatformID: "chat-api",
		Metadata:   scheduleFireMetadata(),
	}
	cap := inboundPermissionCapability(router, event)
	if cap.Interactive {
		t.Fatalf("schedule fire cap = %+v, want non-interactive", cap)
	}
	if cap.DefaultTimeout != interactive.DefaultTimeout {
		t.Fatalf("timeout = %v, want preserved %v", cap.DefaultTimeout, interactive.DefaultTimeout)
	}

	plain := agentkit.MessageEvent{PlatformID: "chat-api"}
	cap = inboundPermissionCapability(router, plain)
	if !cap.Interactive {
		t.Fatalf("normal inbound cap = %+v, want interactive", cap)
	}
}

func TestInboundPermissionCapabilityLeafPlatform(t *testing.T) {
	plat := &stubCapablePlatform{
		id: "cli",
		cap: permission.Capability{Interactive: true},
	}
	event := agentkit.MessageEvent{
		PlatformID: "cli",
		Metadata:   scheduleFireMetadata(),
	}
	cap := inboundPermissionCapability(plat, event)
	if cap.Interactive {
		t.Fatalf("schedule fire cap = %+v, want non-interactive", cap)
	}
}
