package todo

import (
	"context"
	"fmt"
	"strings"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/session"
)

type TodoConfig struct{}

type TodoDeps struct {
	SessionStore agentkit.SessionStore `json:"sessionStore"`
}

type TodoItemInput struct {
	ID     string `json:"id,omitempty" jsonschema:"Stable id of the task; omit on set to derive it from the position"`
	Title  string `json:"title,omitempty" jsonschema:"What needs doing"`
	Status string `json:"status,omitempty" jsonschema:"pending | in_progress | done"`
}

type TodoInput struct {
	Op    string          `json:"op" jsonschema:"list to read the current plan; set to replace it; complete to mark ids done"`
	Items []TodoItemInput `json:"items,omitempty" jsonschema:"Full task list for set; for complete only the ids matter"`
	IDs   []string        `json:"ids,omitempty" jsonschema:"Task ids to mark done when op is complete"`
}

type TodoOutput struct {
	Items       []session.Todo `json:"items"`
	Pending     int            `json:"pending"`
	Total       int            `json:"total"`
	Instruction string         `json:"instruction,omitempty"`
}

// Todo operations.
const (
	todoOpList     = "list"
	todoOpSet      = "set"
	todoOpComplete = "complete"
)

// NewTodo registers tool/todo: Durable task list; the signal an autonomous run uses to decide whether work remains.
//
// Best practices:
//   - op=set replaces the whole list, op=complete closes ids, op=list reads it.
//   - Pair with tool/finish: an empty pending list alone does not end a run.
func NewTodo(_ TodoConfig, deps TodoDeps) (agentkit.Tool, error) {
	if deps.SessionStore == nil {
		return nil, fmt.Errorf("tool/todo requires sessionStore dependency")
	}
	store := deps.SessionStore
	tool, err := agentkit.NewTool[TodoInput, TodoOutput]("todo", func(ctx context.Context, input TodoInput) (TodoOutput, error) {
		sessionID := session.SessionIDFromContext(ctx)
		if sessionID == "" {
			return TodoOutput{}, fmt.Errorf("todo requires a session")
		}
		agentID := session.AgentIDFromContext(ctx)
		sess, err := store.Get(ctx, sessionID)
		if err != nil {
			return TodoOutput{}, err
		}
		events, err := session.ReadAllEvents(ctx, sess)
		if err != nil {
			return TodoOutput{}, err
		}
		current := session.LatestTodos(events)

		op := strings.ToLower(strings.TrimSpace(input.Op))
		switch op {
		case todoOpList, "":
			return todoOutput(current), nil
		case todoOpSet:
			next, err := normalizeTodoItems(input.Items)
			if err != nil {
				return TodoOutput{}, err
			}
			if err := session.AppendTodoUpdate(ctx, sess, agentID, next); err != nil {
				return TodoOutput{}, err
			}
			return todoOutput(next), nil
		case todoOpComplete:
			ids := input.IDs
			for _, item := range input.Items {
				if item.ID != "" {
					ids = append(ids, item.ID)
				}
			}
			if len(ids) == 0 {
				return TodoOutput{}, fmt.Errorf("complete requires at least one id")
			}
			next, missing := completeTodos(current, ids)
			if len(missing) > 0 {
				return TodoOutput{}, fmt.Errorf("unknown todo id(s): %s", strings.Join(missing, ", "))
			}
			if err := session.AppendTodoUpdate(ctx, sess, agentID, next); err != nil {
				return TodoOutput{}, err
			}
			return todoOutput(next), nil
		default:
			return TodoOutput{}, fmt.Errorf("unknown op %q: use list, set or complete", input.Op)
		}
	}).
		Description("Durable task list for multi-step work. Call with op=set to record the plan, op=complete as each task lands, op=list to review. An autonomous run keeps going while tasks remain pending.").
		Build()
	if err != nil {
		return nil, err
	}
	return tool, nil
}

func normalizeTodoItems(items []TodoItemInput) ([]session.Todo, error) {
	if len(items) == 0 {
		return nil, fmt.Errorf("set requires at least one item")
	}
	out := make([]session.Todo, 0, len(items))
	seen := make(map[string]bool, len(items))
	for i, item := range items {
		title := strings.TrimSpace(item.Title)
		if title == "" {
			return nil, fmt.Errorf("item %d requires a title", i+1)
		}
		id := strings.TrimSpace(item.ID)
		if id == "" {
			id = fmt.Sprintf("%d", i+1)
		}
		if seen[id] {
			return nil, fmt.Errorf("duplicate todo id %q", id)
		}
		seen[id] = true
		out = append(out, session.Todo{
			ID:     id,
			Title:  title,
			Status: normalizeTodoStatus(item.Status),
		})
	}
	return out, nil
}

func normalizeTodoStatus(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case session.TodoDone, "completed", "complete":
		return session.TodoDone
	case session.TodoInProgress, "in-progress", "active":
		return session.TodoInProgress
	default:
		return session.TodoPending
	}
}

func completeTodos(current []session.Todo, ids []string) (next []session.Todo, missing []string) {
	index := make(map[string]bool, len(ids))
	for _, id := range ids {
		index[strings.TrimSpace(id)] = true
	}
	next = make([]session.Todo, len(current))
	for i, item := range current {
		next[i] = item
		if index[item.ID] {
			next[i].Status = session.TodoDone
			delete(index, item.ID)
		}
	}
	for id := range index {
		missing = append(missing, id)
	}
	return next, missing
}

func todoOutput(items []session.Todo) TodoOutput {
	pending := session.PendingTodos(items)
	out := TodoOutput{Items: items, Pending: len(pending), Total: len(items)}
	if len(pending) == 0 && len(items) > 0 {
		out.Instruction = "All tasks are done. Call finish to end the run."
	}
	return out
}
