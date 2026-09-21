package compaction

import (
	"testing"

	capcompaction "github.com/lengzhao/agentkit/cap/compaction"
	rtcompaction "github.com/lengzhao/agentkit/runtime/compaction"
)

func mustChain(t *testing.T) capcompaction.Chain {
	t.Helper()
	chain, err := rtcompaction.NewChain(struct{}{})
	if err != nil {
		t.Fatal(err)
	}
	return chain
}
