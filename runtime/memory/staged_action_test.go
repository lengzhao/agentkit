package memory

import "testing"

func TestStagedPendingSummaryStructured(t *testing.T) {
	got := StagedPendingSummary(StagedMemory{
		Action:  StagedActionReplace,
		OldText: "tea",
		Content: "coffee",
	})
	if got != `replace "tea" → coffee` {
		t.Fatalf("got %q", got)
	}
}

func TestLegacyStagedContentSummary(t *testing.T) {
	if LegacyStagedContentSummary("remove:stale") != `remove match "stale"` {
		t.Fatal()
	}
	if LegacyStagedContentSummary("old => new") != `replace "old" → new` {
		t.Fatal()
	}
}
