package llm

import (
	"context"
	"testing"
)

func TestActualModelSlot(t *testing.T) {
	t.Parallel()
	ctx := WithActualModelSlot(context.Background())
	NoteActualModel(ctx, "gpt-4o")
	if got := ActualModelFrom(ctx); got != "gpt-4o" {
		t.Fatalf("actual = %q", got)
	}
	NoteActualModel(ctx, "gpt-4o-mini")
	if got := ActualModelFrom(ctx); got != "gpt-4o-mini" {
		t.Fatalf("actual after overwrite = %q", got)
	}
}

func TestNoteActualModelWithoutSlotIsNoop(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	NoteActualModel(ctx, "gpt-4o")
	if got := ActualModelFrom(ctx); got != "" {
		t.Fatalf("expected noop without slot, got %q", got)
	}
}
