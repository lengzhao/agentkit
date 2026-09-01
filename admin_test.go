package agentkit

import (
	"context"
	"testing"
)

func TestIsAdmin(t *testing.T) {
	if IsAdmin(context.Background()) {
		t.Fatal("expected false without KeyIsAdmin")
	}
	ctx := context.WithValue(context.Background(), KeyIsAdmin, true)
	if !IsAdmin(ctx) {
		t.Fatal("expected true with KeyIsAdmin")
	}
}
