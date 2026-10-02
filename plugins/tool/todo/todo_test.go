package todo

import (
	"testing"

	capsession "github.com/lengzhao/agentkit/cap/session"
)

func TestNormalizeTodoItems(t *testing.T) {
	items, err := normalizeTodoItems([]TodoItemInput{
		{Title: "first"},
		{ID: "a", Title: "second", Status: "in-progress"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if items[0].ID != "1" || items[0].Status != capsession.TodoPending {
		t.Fatalf("first item: %+v", items[0])
	}
	if items[1].Status != capsession.TodoInProgress {
		t.Fatalf("second status: %s", items[1].Status)
	}
	if _, err := normalizeTodoItems(nil); err == nil {
		t.Fatal("empty set must fail")
	}
	if _, err := normalizeTodoItems([]TodoItemInput{{Title: " "}}); err == nil {
		t.Fatal("blank title must fail")
	}
	if _, err := normalizeTodoItems([]TodoItemInput{
		{ID: "x", Title: "a"},
		{ID: "x", Title: "b"},
	}); err == nil {
		t.Fatal("duplicate id must fail")
	}
}

func TestCompleteTodos(t *testing.T) {
	current := []capsession.Todo{
		{ID: "1", Title: "a", Status: capsession.TodoPending},
		{ID: "2", Title: "b", Status: capsession.TodoPending},
	}
	next, missing := completeTodos(current, []string{"2", "nope"})
	if len(missing) != 1 || missing[0] != "nope" {
		t.Fatalf("missing = %v", missing)
	}
	if next[1].Status != capsession.TodoDone || next[0].Status != capsession.TodoPending {
		t.Fatalf("next = %+v", next)
	}
}

func TestTodoOutputInstructionWhenAllDone(t *testing.T) {
	out := todoOutput([]capsession.Todo{
		{ID: "1", Title: "a", Status: capsession.TodoDone},
	})
	if out.Pending != 0 || out.Instruction == "" {
		t.Fatalf("expected finish instruction, got %+v", out)
	}
}

func TestNewTodoRequiresDeps(t *testing.T) {
	if _, err := NewTodo(TodoConfig{}, TodoDeps{}); err == nil {
		t.Fatal("expected error without deps")
	}
}
