package rpcplatform

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/lengzhao/agentkit"
	capsession "github.com/lengzhao/agentkit/cap/session"
)

func TestHandshakeAndTextTurn(t *testing.T) {
	p, buf := newTestPlatform(t, nil)
	mustHandle(t, p, `{"id":"centaur.state","type":"get_state"}`)
	mustHandle(t, p, `{"id":"centaur.model","type":"set_model","modelId":"gpt-5.4","provider":"acme"}`)
	mustHandle(t, p, `{"id":"centaur.reasoning","type":"set_thinking_level","level":"medium"}`)
	mustHandle(t, p, `{"id":"centaur.prompt","type":"prompt","message":"你好","images":[]}`)

	mustSend(t, p, agentkit.EventTurnStart, capsession.TurnStartData{})
	mustSend(t, p, agentkit.EventMessageStart, agentkit.MessageStartPayload{})
	mustSend(t, p, agentkit.EventMessageUpdate, agentkit.MessageUpdatePayload{
		AssistantMessageEvent: agentkit.AssistantMessageEvent{
			Type:         agentkit.AssistantEventTextDelta,
			ContentIndex: 0,
			Delta:        "你好",
		},
	})
	mustSend(t, p, agentkit.EventTurnEnd, capsession.TurnEndData{
		Message: mustJSON(t, agentkit.ModelMessage{
			Role: "assistant",
			Content: []agentkit.ContentPart{{
				Type: "text",
				Text: "你好",
			}},
			StopReason: "stop",
		}),
	})

	lines := decodeLines(t, buf.Bytes())
	if len(lines) != 7 {
		t.Fatalf("lines = %d, want 7: %s", len(lines), buf.String())
	}
	if lines[0]["id"] != "centaur.state" || lines[0]["command"] != "get_state" || lines[0]["success"] != true {
		t.Fatalf("get_state = %#v", lines[0])
	}
	data := lines[0]["data"].(map[string]any)
	if data["sessionId"] != "11111111-1111-1111-1111-111111111111" {
		t.Fatalf("sessionId = %#v", data["sessionId"])
	}
	if data["isStreaming"] != false || data["pendingMessageCount"] != float64(0) {
		t.Fatalf("state flags = %#v", data)
	}
	model := data["model"].(map[string]any)
	if model["id"] != "gpt-5.4" || model["provider"] != "acme" || model["api"] != "openai-responses" || model["baseUrl"] != "https://models.acme.example/v1" {
		t.Fatalf("model = %#v", model)
	}
	if !filepath.IsAbs(data["sessionFile"].(string)) {
		t.Fatalf("sessionFile = %#v", data["sessionFile"])
	}
	if lines[2]["command"] != "set_thinking_level" || lines[2]["success"] != true {
		t.Fatalf("thinking = %#v", lines[2])
	}
	if p.effort != "medium" {
		t.Fatalf("effort = %q", p.effort)
	}
	if lines[3]["command"] != "prompt" || lines[3]["success"] != true {
		t.Fatalf("prompt = %#v", lines[3])
	}
	if lines[4]["type"] != "agent_start" {
		t.Fatalf("agent_start = %#v", lines[4])
	}
	upd := lines[5]["assistantMessageEvent"].(map[string]any)
	if lines[5]["type"] != "message_update" || upd["type"] != "text_delta" || upd["delta"] != "你好" || upd["contentIndex"] != float64(0) {
		t.Fatalf("update = %#v", lines[5])
	}
	end := lines[6]
	if end["type"] != "agent_end" {
		t.Fatalf("agent_end = %#v", end)
	}
	msg := end["messages"].([]any)[0].(map[string]any)
	if msg["stopReason"] != "stop" || msg["timestamp"] != float64(1) || msg["model"] != "gpt-5.4" {
		t.Fatalf("assistant = %#v", msg)
	}
	assertNoForbidden(t, lines[3:])
}

func TestSetModelRejectsUnknownProviderAndModel(t *testing.T) {
	p, buf := newTestPlatform(t, nil)
	mustHandle(t, p, `{"id":"centaur.model","type":"set_model","modelId":"gpt-5.4","provider":"other"}`)
	mustHandle(t, p, `{"id":"centaur.model","type":"set_model","modelId":"nope","provider":"acme"}`)
	lines := decodeLines(t, buf.Bytes())
	if lines[0]["success"] != false || lines[1]["success"] != false {
		t.Fatalf("responses = %s", buf.String())
	}
	if p.modelID != "gpt-5.4" {
		t.Fatalf("model changed to %s", p.modelID)
	}
}

func TestSetModelBindsAllowedModel(t *testing.T) {
	bind := &memBind{}
	p, buf := newTestPlatform(t, bind)
	mustHandle(t, p, `{"id":"centaur.model","type":"set_model","modelId":"gpt-5.4-mini","provider":"acme"}`)
	lines := decodeLines(t, buf.Bytes())
	if lines[0]["success"] != true {
		t.Fatalf("set_model = %#v", lines[0])
	}
	if bind.model != "gpt-5.4-mini" || bind.id != "rpc:11111111-1111-1111-1111-111111111111" {
		t.Fatalf("bind = %s %s", bind.id, bind.model)
	}
	mustHandle(t, p, `{"id":"centaur.state","type":"get_state"}`)
	state := decodeLines(t, buf.Bytes())
	model := state[1]["data"].(map[string]any)["model"].(map[string]any)
	if model["id"] != "gpt-5.4-mini" {
		t.Fatalf("get_state model = %#v", model)
	}
}

func TestToolTurnStopReasonIsNotToolUse(t *testing.T) {
	p, buf := newTestPlatform(t, nil)
	mustHandle(t, p, `{"id":"centaur.prompt","type":"prompt","message":"查一下","images":[]}`)
	mustSend(t, p, agentkit.EventTurnStart, capsession.TurnStartData{})
	mustSend(t, p, agentkit.EventMessageStart, agentkit.MessageStartPayload{})
	mustSend(t, p, agentkit.EventMessageUpdate, agentkit.MessageUpdatePayload{
		AssistantMessageEvent: agentkit.AssistantMessageEvent{
			Type: agentkit.AssistantEventToolCallStart,
			ToolCall: &agentkit.ToolCall{
				ID:   "call1",
				Name: "native",
			},
		},
	})
	mustSend(t, p, agentkit.EventMessageUpdate, agentkit.MessageUpdatePayload{
		AssistantMessageEvent: agentkit.AssistantMessageEvent{
			Type: agentkit.AssistantEventToolCallEnd,
			ToolCall: &agentkit.ToolCall{
				ID:    "call1",
				Name:  "native",
				Input: json.RawMessage(`{"q":"x"}`),
			},
		},
	})
	mustSend(t, p, agentkit.EventToolResult, agentkit.ToolResult{
		ID:      "call1",
		Name:    "native",
		Content: "ok",
		Audit:   map[string]string{"decision": "deny"},
	})
	mustSend(t, p, agentkit.EventMessageEnd, agentkit.MessageEndPayload{Message: agentkit.ModelMessage{
		Role:       "assistant",
		StopReason: "toolUse",
		ToolCalls: []agentkit.ToolCall{{
			ID:    "call1",
			Name:  "native",
			Input: json.RawMessage(`{"q":"x"}`),
		}},
	}})
	mustSend(t, p, agentkit.EventMessageStart, agentkit.MessageStartPayload{})
	mustSend(t, p, agentkit.EventMessageUpdate, agentkit.MessageUpdatePayload{
		AssistantMessageEvent: agentkit.AssistantMessageEvent{
			Type:         agentkit.AssistantEventTextDelta,
			ContentIndex: 0,
			Delta:        "done",
		},
	})
	mustSend(t, p, agentkit.EventTurnEnd, capsession.TurnEndData{
		Message: mustJSON(t, agentkit.ModelMessage{
			Role:       "assistant",
			StopReason: "stop",
			Content:    []agentkit.ContentPart{{Type: "text", Text: "done"}},
		}),
	})

	lines := decodeLines(t, buf.Bytes())
	var starts, ends int
	var sawTool bool
	for _, line := range lines {
		switch line["type"] {
		case "agent_start":
			starts++
		case "agent_end":
			ends++
			msg := line["messages"].([]any)[0].(map[string]any)
			if msg["stopReason"] != "stop" {
				t.Fatalf("stopReason = %#v", msg["stopReason"])
			}
		case "tool_execution_start":
			sawTool = true
			if line["toolCallId"] != "call1" || line["toolName"] != "native" {
				t.Fatalf("start = %#v", line)
			}
		case "tool_execution_end":
			if line["toolCallId"] != "call1" || line["isError"] != true {
				t.Fatalf("end = %#v", line)
			}
		case "message_update":
			ev := line["assistantMessageEvent"].(map[string]any)
			if ev["type"] == "toolcall_start" || ev["type"] == "toolcall_end" {
				t.Fatalf("tool delta leaked: %#v", line)
			}
		}
	}
	if starts != 1 || ends != 1 || !sawTool {
		t.Fatalf("starts=%d ends=%d tool=%v\n%s", starts, ends, sawTool, buf.String())
	}
}

func TestAbortAfterStart(t *testing.T) {
	p, buf := newTestPlatform(t, nil)
	ctrl := &stubCancel{}
	mustHandle(t, p, `{"id":"centaur.prompt","type":"prompt","message":"hi","images":[]}`)
	ctx := context.WithValue(context.Background(), agentkit.KeySessionControl, ctrl)
	if err := p.Send(ctx, outbound(agentkit.EventTurnStart, capsession.TurnStartData{})); err != nil {
		t.Fatal(err)
	}
	mustHandle(t, p, `{"id":"centaur.abort","type":"abort"}`)
	if !ctrl.called {
		t.Fatal("cancel was not called")
	}
	mustSend(t, p, agentkit.EventTurnEnd, capsession.TurnEndData{Cancelled: true})
	lines := decodeLines(t, buf.Bytes())
	var sawAck, sawEnd bool
	for _, line := range lines {
		if line["command"] == "abort" {
			if line["success"] != true {
				t.Fatalf("abort = %#v", line)
			}
			sawAck = true
		}
		if line["type"] == "agent_end" {
			sawEnd = true
			msg := line["messages"].([]any)[0].(map[string]any)
			if msg["stopReason"] != "aborted" || msg["role"] != "assistant" {
				t.Fatalf("end message = %#v", msg)
			}
		}
	}
	if !sawAck || !sawEnd {
		t.Fatalf("ack=%v end=%v\n%s", sawAck, sawEnd, buf.String())
	}
}

func TestAbortBeforeStartClosesOnce(t *testing.T) {
	p, buf := newTestPlatform(t, nil)
	mustHandle(t, p, `{"id":"centaur.prompt","type":"prompt","message":"hi","images":[]}`)
	mustHandle(t, p, `{"id":"centaur.abort","type":"abort"}`)
	mustSend(t, p, agentkit.EventTurnStart, capsession.TurnStartData{})
	mustSend(t, p, agentkit.EventTurnEnd, capsession.TurnEndData{})
	var starts, ends int
	for _, line := range decodeLines(t, buf.Bytes()) {
		switch line["type"] {
		case "agent_start":
			starts++
		case "agent_end":
			ends++
			msg := line["messages"].([]any)[0].(map[string]any)
			if msg["stopReason"] != "aborted" {
				t.Fatalf("stopReason = %#v", msg["stopReason"])
			}
		}
	}
	if starts != 1 || ends != 1 {
		t.Fatalf("starts=%d ends=%d\n%s", starts, ends, buf.String())
	}
}

func TestSteerWhileRunningDoesNotStartAnotherTurn(t *testing.T) {
	p, buf := newTestPlatform(t, nil)
	mustHandle(t, p, `{"id":"centaur.prompt","type":"prompt","message":"hi","images":[]}`)
	mustSend(t, p, agentkit.EventTurnStart, capsession.TurnStartData{})
	mustHandle(t, p, `{"id":"centaur.steer","type":"steer","message":"补充","images":[]}`)
	ev, err := p.inbox.Receive(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	ev2, err := p.inbox.Receive(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if ev.Message.Content[0].Text != "hi" || ev2.Message.Content[0].Text != "补充" {
		t.Fatalf("messages = %#v %#v", ev.Message, ev2.Message)
	}
	var starts int
	for _, line := range decodeLines(t, buf.Bytes()) {
		if line["type"] == "agent_start" {
			starts++
		}
		if line["command"] == "steer" && line["success"] != true {
			t.Fatalf("steer = %#v", line)
		}
	}
	if starts != 1 {
		t.Fatalf("agent_start count = %d", starts)
	}
}

func TestSteerWhileIdleIsRejected(t *testing.T) {
	p, buf := newTestPlatform(t, nil)
	mustHandle(t, p, `{"id":"centaur.steer","type":"steer","message":"x","images":[]}`)
	lines := decodeLines(t, buf.Bytes())
	if len(lines) != 1 || lines[0]["success"] != false || lines[0]["command"] != "steer" {
		t.Fatalf("steer = %#v", lines)
	}
}

func TestPromptWhileBusyIsRejected(t *testing.T) {
	p, buf := newTestPlatform(t, nil)
	mustHandle(t, p, `{"id":"centaur.prompt","type":"prompt","message":"a","images":[]}`)
	mustHandle(t, p, `{"id":"centaur.prompt","type":"prompt","message":"b","images":[]}`)
	lines := decodeLines(t, buf.Bytes())
	if lines[0]["success"] != true || lines[1]["success"] != false {
		t.Fatalf("prompts = %#v %#v", lines[0], lines[1])
	}
}

func TestErrorBeforeAgentStart(t *testing.T) {
	p, buf := newTestPlatform(t, nil)
	mustHandle(t, p, `{"id":"centaur.prompt","type":"prompt","message":"a","images":[]}`)
	mustSend(t, p, "error", map[string]string{"error": "boom"})
	lines := decodeLines(t, buf.Bytes())
	last := lines[len(lines)-1]
	if last["type"] != "centaur_error" || last["id"] != "centaur.prompt" || last["code"] != "pi_prompt_failed" {
		t.Fatalf("error = %#v", last)
	}
	for _, line := range lines {
		if line["type"] == "agent_end" || line["type"] == "agent_start" {
			t.Fatalf("turn event leaked: %#v", line)
		}
	}
}

func TestErrorThenTurnEndDoesNotEmitAgentEnd(t *testing.T) {
	p, buf := newTestPlatform(t, nil)
	mustHandle(t, p, `{"id":"centaur.prompt","type":"prompt","message":"a","images":[]}`)
	mustSend(t, p, "error", map[string]string{"error": "boom"})
	mustSend(t, p, agentkit.EventTurnEnd, capsession.TurnEndData{})
	for _, line := range decodeLines(t, buf.Bytes()) {
		if line["type"] == "agent_end" || line["type"] == "agent_start" {
			t.Fatalf("turn event leaked: %#v", line)
		}
	}
	mustHandle(t, p, `{"id":"centaur.prompt","type":"prompt","message":"b","images":[]}`)
	lines := decodeLines(t, buf.Bytes())
	if lines[len(lines)-1]["success"] != true {
		t.Fatalf("next prompt = %#v", lines[len(lines)-1])
	}
}

func TestMessageEndOnceAndDropsHighContentIndex(t *testing.T) {
	p, buf := newTestPlatform(t, nil)
	mustHandle(t, p, `{"id":"centaur.prompt","type":"prompt","message":"a","images":[]}`)
	mustSend(t, p, agentkit.EventTurnStart, capsession.TurnStartData{})
	mustSend(t, p, agentkit.EventMessageUpdate, agentkit.MessageUpdatePayload{
		AssistantMessageEvent: agentkit.AssistantMessageEvent{
			Type:         agentkit.AssistantEventTextDelta,
			ContentIndex: 4097,
			Delta:        "nope",
		},
	})
	mustSend(t, p, agentkit.EventMessageUpdate, agentkit.MessageUpdatePayload{
		AssistantMessageEvent: agentkit.AssistantMessageEvent{
			Type:         agentkit.AssistantEventTextDelta,
			ContentIndex: 0,
			Delta:        "ok",
		},
	})
	end := agentkit.MessageEndPayload{Message: agentkit.ModelMessage{
		Role:    "assistant",
		Content: []agentkit.ContentPart{{Type: "text", Text: "ok"}},
	}}
	mustSend(t, p, agentkit.EventMessageEnd, end)
	mustSend(t, p, agentkit.EventMessageEnd, end)
	var updates, ends int
	for _, line := range decodeLines(t, buf.Bytes()) {
		switch line["type"] {
		case "message_update":
			updates++
			ev := line["assistantMessageEvent"].(map[string]any)
			if ev["delta"] == "nope" {
				t.Fatal("dropped delta was written")
			}
		case "message_end":
			ends++
		}
	}
	if updates != 1 || ends != 1 {
		t.Fatalf("updates=%d ends=%d\n%s", updates, ends, buf.String())
	}
}

func TestGetStateRejectedWhileRunning(t *testing.T) {
	p, buf := newTestPlatform(t, nil)
	mustHandle(t, p, `{"id":"centaur.prompt","type":"prompt","message":"a","images":[]}`)
	mustHandle(t, p, `{"id":"centaur.state","type":"get_state"}`)
	lines := decodeLines(t, buf.Bytes())
	if lines[1]["success"] != false || lines[1]["command"] != "get_state" {
		t.Fatalf("get_state = %#v", lines[1])
	}
}

func TestPermissionIsNotInteractive(t *testing.T) {
	p, _ := newTestPlatform(t, nil)
	if p.PermissionCapability().Interactive {
		t.Fatal("rpc platform is interactive")
	}
}

func TestThinkingLevelRejected(t *testing.T) {
	p, buf := newTestPlatform(t, nil)
	mustHandle(t, p, `{"id":"centaur.reasoning","type":"set_thinking_level","level":"max"}`)
	lines := decodeLines(t, buf.Bytes())
	if lines[0]["success"] != false {
		t.Fatalf("level = %#v", lines[0])
	}
}

type memBind struct {
	id    agentkit.SessionID
	model string
}

func (m *memBind) Get(context.Context, agentkit.SessionID) (agentkit.Session, error) {
	return nil, context.Canceled
}

func (m *memBind) AgentBind(context.Context, agentkit.SessionID) (agentkit.AgentID, error) {
	return "", nil
}

func (m *memBind) SetAgentBind(context.Context, agentkit.SessionID, agentkit.AgentID) error {
	return nil
}

func (m *memBind) ModelBind(context.Context, agentkit.SessionID) (string, error) {
	return m.model, nil
}

func (m *memBind) SetModelBind(_ context.Context, id agentkit.SessionID, model string) error {
	m.id = id
	m.model = model
	return nil
}

type stubCancel struct{ called bool }

func (s *stubCancel) Cancel(context.Context, string) error {
	s.called = true
	return nil
}

func newTestPlatform(t *testing.T, bind agentkit.SessionStore) (*Platform, *bytes.Buffer) {
	t.Helper()
	dir := t.TempDir()
	var buf bytes.Buffer
	raw, err := New(Config{
		SessionFile:   filepath.Join(dir, "11111111-1111-1111-1111-111111111111.jsonl"),
		Model:         "gpt-5.4",
		ModelProvider: "acme",
		BaseURL:       "https://models.acme.example/v1",
		WireAPI:       "openai-responses",
		AllowedModels: map[string]json.RawMessage{
			"gpt-5.4-mini": nil,
		},
	}, Deps{SessionStore: bind})
	if err != nil {
		t.Fatal(err)
	}
	p := raw.(*Platform)
	p.out = &buf
	return p, &buf
}

func mustHandle(t *testing.T, p *Platform, line string) {
	t.Helper()
	if err := p.handleLine(line); err != nil {
		t.Fatal(err)
	}
}

func mustSend(t *testing.T, p *Platform, typ agentkit.EventType, payload any) {
	t.Helper()
	if err := p.Send(context.Background(), outbound(typ, payload)); err != nil {
		t.Fatal(err)
	}
}

func outbound(typ agentkit.EventType, payload any) agentkit.OutboundEvent {
	return agentkit.OutboundEvent{Type: typ, Data: mustJSON(nil, payload)}
}

func mustJSON(t *testing.T, v any) json.RawMessage {
	raw, err := json.Marshal(v)
	if err != nil {
		if t != nil {
			t.Fatal(err)
		}
		panic(err)
	}
	return raw
}

func decodeLines(t *testing.T, raw []byte) []map[string]any {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(raw))
	var lines []map[string]any
	for dec.More() {
		var line map[string]any
		if err := dec.Decode(&line); err != nil {
			t.Fatalf("decode %s: %v", raw, err)
		}
		lines = append(lines, line)
	}
	return lines
}

func assertNoForbidden(t *testing.T, lines []map[string]any) {
	t.Helper()
	for _, line := range lines {
		switch line["type"] {
		case "extension_error", "agent_settled", "turn_end":
			t.Fatalf("forbidden event %#v", line)
		}
		switch line["id"] {
		case "centaur.state", "centaur.model", "centaur.reasoning":
			if line["type"] != "response" {
				t.Fatalf("handshake id reappeared: %#v", line)
			}
		}
	}
}
