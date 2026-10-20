package acpremote

import (
	"strings"
	"testing"

	acp "github.com/coder/acp-go-sdk"
)

func TestACPErrorDetailRedactsSensitiveData(t *testing.T) {
	err := wrapACPStageError("session/new", acp.NewAuthRequired(map[string]any{
		"reason":      "login required",
		"accessToken": "super-secret",
	}))
	detail := acpErrorDetail(err)
	if !strings.Contains(detail, "login required") {
		t.Fatalf("expected non-sensitive detail: %s", detail)
	}
	if strings.Contains(detail, "super-secret") {
		t.Fatalf("leaked secret in detail: %s", detail)
	}
	if !strings.Contains(detail, "[redacted]") {
		t.Fatalf("expected redacted marker: %s", detail)
	}
}
