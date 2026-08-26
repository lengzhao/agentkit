package permission

import (
	"testing"
	"time"
)

func TestEffectiveTimeout(t *testing.T) {
	t.Parallel()

	req := Request{Kind: KindAllowDeny}
	cap := Capability{Interactive: true}

	if got := EffectiveTimeout(req, cap); got != DefaultTimeout {
		t.Fatalf("interactive default = %v, want %v", got, DefaultTimeout)
	}

	req.Timeout = 30 * time.Second
	if got := EffectiveTimeout(req, cap); got != 30*time.Second {
		t.Fatalf("request override = %v", got)
	}

	req.Timeout = 0
	cap.DefaultTimeout = 2 * time.Minute
	if got := EffectiveTimeout(req, cap); got != 2*time.Minute {
		t.Fatalf("capability override = %v", got)
	}

	cap.Interactive = false
	cap.DefaultTimeout = 0
	if got := EffectiveTimeout(req, cap); got != 0 {
		t.Fatalf("non-interactive = %v, want 0", got)
	}
}
