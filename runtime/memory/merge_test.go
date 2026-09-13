package memory

import (
	"testing"

	capmemory "github.com/lengzhao/agentkit/cap/memory"
)

func TestMergeMemoryAddDuplicate(t *testing.T) {
	entries := []MemoryEntry{{Content: "call me 小飞"}}
	next, out := MergeMemoryAdd(entries, "call me 小飞")
	if out != capmemory.AddOutcomeDuplicate || len(next) != 1 {
		t.Fatalf("out=%s next=%v", out, next)
	}
}

func TestMergeMemoryAddSubstringDuplicate(t *testing.T) {
	entries := []MemoryEntry{{Content: "The user wants the assistant to be called 小飞."}}
	_, out := MergeMemoryAdd(entries, "called 小飞")
	if out != capmemory.AddOutcomeDuplicate {
		t.Fatal(out)
	}
}

func TestMergeMemoryAddReplaceLonger(t *testing.T) {
	entries := []MemoryEntry{{Content: "likes tea"}}
	next, out := MergeMemoryAdd(entries, "likes tea and coffee")
	if out != capmemory.AddOutcomeReplaced || len(next) != 1 || next[0].Content != "likes tea and coffee" {
		t.Fatalf("out=%s next=%v", out, next)
	}
}
