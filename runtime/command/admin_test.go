package command

import (
	"context"
	"testing"

	"github.com/lengzhao/agentkit"
)

func TestIsAdmin(t *testing.T) {
	if IsAdmin(context.Background()) {
		t.Fatal("expected false without KeyIsAdmin")
	}
	ctx := context.WithValue(context.Background(), agentkit.KeyIsAdmin, true)
	if !IsAdmin(ctx) {
		t.Fatal("expected true with KeyIsAdmin")
	}
}
