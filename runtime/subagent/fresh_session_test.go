package subagent

import (
	"context"
	"github.com/lengzhao/agentkit"
	capsubagent "github.com/lengzhao/agentkit/cap/subagent"
	"github.com/lengzhao/agentkit/runtime/rctx"
	"testing"
)

// An immutable parent view models delegations started from the same event
// snapshot. Appends still go to the real parent history.
type parentSnapshot struct{ agentkit.Session }

func (s parentSnapshot) Read(context.Context, agentkit.EventSeq) ([]agentkit.SessionEvent, error) {
	return nil, nil
}

func TestFreshSessionSeparatesDelegationsFromSameParentSnapshot(t *testing.T) {
	for _, fresh := range []bool{false, true} {
		t.Run(map[bool]string{false: "legacy", true: "fresh"}[fresh], func(t *testing.T) {
			base, store, _ := newLoopSpawner(t, false, "answer")
			spawner, err := NewLoopAgent(LoopAgentConfig{FreshSession: fresh, Agents: base.entries}, LoopAgentDeps{SessionStore: store, Agents: []agentkit.Agent{base.agents["cursor"]}})
			if err != nil {
				t.Fatal(err)
			}
			ctx := loopParentCtx()
			parent, err := store.Get(ctx, rctx.SessionIDFromContext(ctx))
			if err != nil {
				t.Fatal(err)
			}
			ctx = rctx.WithSession(ctx, parentSnapshot{parent})
			a, err := spawner.Run(ctx, capsubagent.Request{Agent: "cursor", Task: "first"})
			if err != nil {
				t.Fatal(err)
			}
			b, err := spawner.Run(ctx, capsubagent.Request{Agent: "cursor", Task: "second"})
			if err != nil {
				t.Fatal(err)
			}
			if (a.Session != b.Session) != fresh {
				t.Fatalf("fresh=%v histories=%q,%q", fresh, a.Session, b.Session)
			}
		})
	}
}
