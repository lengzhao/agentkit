package skill_test

import (
	"testing"

	rtskill "github.com/lengzhao/agentkit/runtime/skill"
)

func TestSanitizeRelativePath(t *testing.T) {
	t.Parallel()

	if _, err := rtskill.SanitizeRelativePath("../secret.md"); err == nil {
		t.Fatal("expected escape rejection")
	}
	if got, err := rtskill.SanitizeRelativePath("reference.md"); err != nil || got != "reference.md" {
		t.Fatalf("got %q err=%v", got, err)
	}
}
