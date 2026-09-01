package chatapi

import (
	"testing"
	"time"

	"github.com/lengzhao/agentkit/cap/permission"
)

func TestChatAPIPermissionCapabilityDefaultInteractive(t *testing.T) {
	t.Parallel()

	p, err := New(Config{}, Deps{})
	if err != nil {
		t.Fatal(err)
	}
	cap := p.(*Platform).PermissionCapability()
	if !cap.Interactive {
		t.Fatalf("cap = %+v, want interactive default true", cap)
	}
	if cap.AnswerScope != permission.ScopeAsker {
		t.Fatalf("AnswerScope = %v", cap.AnswerScope)
	}
}

func TestChatAPIPermissionCapabilityConfigurable(t *testing.T) {
	t.Parallel()

	falseVal := false
	p, err := New(Config{
		Interactive:        &falseVal,
		InteractionTimeout: "2m",
	}, Deps{})
	if err != nil {
		t.Fatal(err)
	}
	cap := p.(*Platform).PermissionCapability()
	if cap.Interactive {
		t.Fatalf("cap = %+v, want non-interactive", cap)
	}
	if cap.DefaultTimeout != 2*time.Minute {
		t.Fatalf("DefaultTimeout = %v", cap.DefaultTimeout)
	}
}
