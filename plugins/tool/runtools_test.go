package tool_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/plugins/tool/finish"
	"github.com/lengzhao/agentkit/plugins/tool/testutil"
	"github.com/lengzhao/agentkit/plugins/tool/todo"
	"github.com/lengzhao/agentkit/runtime/session"
)

type singleSessionStore struct {
	sess agentkit.Session
}

func (s singleSessionStore) Get(context.Context, agentkit.SessionID) (agentkit.Session, error) {
	return s.sess, nil
}

func newRunToolsFixture(t *testing.T) (todoTool, finishTool agentkit.Tool, sess agentkit.Session, ctx context.Context) {
	t.Helper()
	sess, err := session.NewMemory(session.MemoryConfig{ID: "test:runtools"})
	if err != nil {
		t.Fatal(err)
	}
	store := singleSessionStore{sess: sess}
	todoTool, err = todo.NewTodo(todo.TodoConfig{}, todo.TodoDeps{SessionStore: store})
	if err != nil {
		t.Fatal(err)
	}
	finishTool, err = finish.NewFinish(finish.FinishConfig{}, finish.FinishDeps{SessionStore: store})
	if err != nil {
		t.Fatal(err)
	}
	ctx = context.WithValue(context.Background(), agentkit.KeySessionID, sess.ID())
	return todoTool, finishTool, sess, ctx
}

func callTool(t *testing.T, ctx context.Context, tl agentkit.Tool, input string) string {
	return testutil.CallTool(t, ctx, tl, input)
}

func TestTodoSetCompleteAndList(t *testing.T) {
	t.Parallel()

	todoTool, _, sess, ctx := newRunToolsFixture(t)

	out := callTool(t, ctx, todoTool, `{"op":"set","items":[{"id":"1","title":"first"},{"title":"second"}]}`)
	var set todo.TodoOutput
	if err := json.Unmarshal([]byte(out), &set); err != nil {
		t.Fatalf("decode set output: %v (%s)", err, out)
	}
	if set.Total != 2 || set.Pending != 2 {
		t.Fatalf("after set: total=%d pending=%d, want 2/2", set.Total, set.Pending)
	}
	if set.Items[1].ID != "2" {
		t.Fatalf("derived id = %q, want %q", set.Items[1].ID, "2")
	}

	out = callTool(t, ctx, todoTool, `{"op":"complete","ids":["1"]}`)
	var done todo.TodoOutput
	if err := json.Unmarshal([]byte(out), &done); err != nil {
		t.Fatalf("decode complete output: %v (%s)", err, out)
	}
	if done.Pending != 1 {
		t.Fatalf("pending after complete = %d, want 1", done.Pending)
	}

	out = callTool(t, ctx, todoTool, `{"op":"complete","ids":["2"]}`)
	var all todo.TodoOutput
	if err := json.Unmarshal([]byte(out), &all); err != nil {
		t.Fatalf("decode output: %v (%s)", err, out)
	}
	if all.Pending != 0 {
		t.Fatalf("pending = %d, want 0", all.Pending)
	}
	if !strings.Contains(all.Instruction, "finish") {
		t.Fatalf("expected a nudge to call finish, got %q", all.Instruction)
	}

	events, err := session.ReadAllEvents(ctx, sess)
	if err != nil {
		t.Fatal(err)
	}
	updates := 0
	for _, ev := range events {
		if ev.Type == agentkit.EventTodoUpdate {
			updates++
		}
	}
	if updates != 3 {
		t.Fatalf("todo/update events = %d, want 3", updates)
	}
	if got := session.PendingTodos(session.LatestTodos(events)); len(got) != 0 {
		t.Fatalf("pending todos on the log = %+v, want none", got)
	}
}

func TestTodoRejectsBadInput(t *testing.T) {
	t.Parallel()

	todoTool, _, _, ctx := newRunToolsFixture(t)

	if out := callTool(t, ctx, todoTool, `{"op":"set","items":[]}`); !strings.Contains(out, "at least one item") {
		t.Fatalf("empty set = %q", out)
	}
	if out := callTool(t, ctx, todoTool, `{"op":"complete","ids":["nope"]}`); !strings.Contains(out, "unknown todo id") {
		t.Fatalf("unknown id = %q", out)
	}
	if out := callTool(t, ctx, todoTool, `{"op":"frobnicate"}`); !strings.Contains(out, "unknown op") {
		t.Fatalf("bad op = %q", out)
	}
	if out := callTool(t, ctx, todoTool, `{"op":"set","items":[{"id":"x","title":"a"},{"id":"x","title":"b"}]}`); !strings.Contains(out, "duplicate") {
		t.Fatalf("duplicate id = %q", out)
	}
}

func TestFinishRecordsRunFinish(t *testing.T) {
	t.Parallel()

	_, finishTool, sess, ctx := newRunToolsFixture(t)

	out := callTool(t, ctx, finishTool, `{"status":"blocked","summary":"missing credentials"}`)
	if !strings.Contains(out, session.FinishBlocked) {
		t.Fatalf("finish output = %q", out)
	}

	events, err := session.ReadAllEvents(ctx, sess)
	if err != nil {
		t.Fatal(err)
	}
	data := session.FinishAfter(events, 0)
	if data == nil {
		t.Fatal("no run/finish event recorded")
	}
	if data.Status != session.FinishBlocked || data.Summary != "missing credentials" {
		t.Fatalf("run/finish = %+v", data)
	}
}

func TestFinishDefaultsToCompletedAndRequiresSummary(t *testing.T) {
	t.Parallel()

	_, finishTool, sess, ctx := newRunToolsFixture(t)

	if out := callTool(t, ctx, finishTool, `{"summary":"all done"}`); !strings.Contains(out, session.FinishCompleted) {
		t.Fatalf("finish without status = %q, want completed", out)
	}
	if out := callTool(t, ctx, finishTool, `{"status":"completed"}`); !strings.Contains(out, "requires a summary") {
		t.Fatalf("finish without summary = %q", out)
	}

	events, err := session.ReadAllEvents(ctx, sess)
	if err != nil {
		t.Fatal(err)
	}
	finishes := 0
	for _, ev := range events {
		if ev.Type == agentkit.EventRunFinish {
			finishes++
		}
	}
	if finishes != 1 {
		t.Fatalf("run/finish events = %d, want 1 (the rejected call must not log)", finishes)
	}
}
