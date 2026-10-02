package todo

import (
	"encoding/json"
	"testing"

	"github.com/lengzhao/agentkit"
	capsession "github.com/lengzhao/agentkit/cap/session"
	"github.com/lengzhao/agentkit/runtime/session/sessevents"
	"github.com/lengzhao/agentkit/testing/agenttest"
)

func TestTodoToolSetListComplete(t *testing.T) {
	sessionID := agentkit.SessionID("test:todo:session")
	store, _ := agenttest.TempFileStore(t)
	events, err := sessevents.New()
	if err != nil {
		t.Fatal(err)
	}
	tool, err := NewTodo(TodoConfig{}, TodoDeps{SessionStore: store, SessionEvents: events})
	if err != nil {
		t.Fatal(err)
	}
	ctx := agenttest.TurnContext(sessionID, "agent-1")

	raw := agenttest.CallTool(t, ctx, tool, `{"op":"set","items":[{"title":"one"},{"id":"2","title":"two"}]}`)
	var setOut TodoOutput
	if err := json.Unmarshal([]byte(raw), &setOut); err != nil {
		t.Fatal(err)
	}
	if setOut.Total != 2 || setOut.Pending != 2 {
		t.Fatalf("after set: %+v", setOut)
	}

	raw = agenttest.CallTool(t, ctx, tool, `{"op":"list"}`)
	var listOut TodoOutput
	if err := json.Unmarshal([]byte(raw), &listOut); err != nil {
		t.Fatal(err)
	}
	if listOut.Total != 2 {
		t.Fatalf("list: %+v", listOut)
	}

	raw = agenttest.CallTool(t, ctx, tool, `{"op":"complete","ids":["2"]}`)
	var doneOut TodoOutput
	if err := json.Unmarshal([]byte(raw), &doneOut); err != nil {
		t.Fatal(err)
	}
	if doneOut.Pending != 1 {
		t.Fatalf("after complete: %+v", doneOut)
	}
	for _, item := range doneOut.Items {
		if item.ID == "2" && item.Status != capsession.TodoDone {
			t.Fatalf("item 2 not done: %+v", item)
		}
	}
}
