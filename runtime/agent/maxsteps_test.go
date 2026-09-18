package agent

import (
	"errors"
	"testing"
)

func TestResolveMaxSteps(t *testing.T) {
	t.Parallel()
	if resolveMaxSteps(nil) != defaultMaxSteps {
		t.Fatalf("nil config = %d, want %d", resolveMaxSteps(nil), defaultMaxSteps)
	}
	zero := 0
	if resolveMaxSteps(&zero) != 0 {
		t.Fatalf("zero = %d, want unlimited", resolveMaxSteps(&zero))
	}
	five := 5
	if resolveMaxSteps(&five) != 5 {
		t.Fatalf("five = %d, want 5", resolveMaxSteps(&five))
	}
	if StepLimitUserMessage(3) == "" {
		t.Fatal("step limit notice should not be empty")
	}
	if !IsStepLimitError(newStepLimitError(3)) {
		t.Fatal("expected step limit error")
	}
	if IsStepLimitError(errors.New("other")) {
		t.Fatal("unexpected step limit error")
	}
}
