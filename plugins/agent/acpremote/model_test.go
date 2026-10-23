package acpremote

import (
	"context"
	"encoding/json"
	"github.com/lengzhao/agentkit/testing/agenttest"
	"os"
	"testing"
	"time"

	acp "github.com/coder/acp-go-sdk"
	"github.com/lengzhao/agentkit"
)

func modelOptions(t *testing.T, raw string) []acp.SessionConfigOption {
	t.Helper()
	var options []acp.SessionConfigOption
	if err := json.Unmarshal([]byte(raw), &options); err != nil {
		t.Fatal(err)
	}
	return options
}

func TestModelFromConfig(t *testing.T) {
	for _, tc := range []struct{ name, raw, want string }{
		{"missing", "[]", ""},
		{"category", `[{"id":"llm","category":"model","type":"select","currentValue":"remote-model","options":[]}]`, "remote-model"},
		{"id", `[{"id":"model","type":"select","currentValue":"another-model","options":[]}]`, "another-model"},
		{"mode", `[{"id":"mode","category":"mode","type":"select","currentValue":"agent","options":[]}]`, ""},
		{"empty", `[{"id":"model","type":"select","currentValue":"  ","options":[]}]`, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := modelFromConfig(modelOptions(t, tc.raw)); got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestModelNotification(t *testing.T) {
	b := &bridge{}
	proc := &subprocess{}
	b.proc.Store(proc)
	var events []agentkit.OutboundEvent
	e := newUpdateEmitter(mustToolkit(t), context.Background(), "local", "remote", func(_ context.Context, ev agentkit.OutboundEvent) error {
		if ev.Type == agentkit.EventMessageUpdate {
			events = append(events, ev)
		}
		return nil
	}, agentkit.ModelMessage{})
	b.setTurn(turnState{emitter: e, sessionID: "local"})
	client := &bridgeClient{bridge: b}
	update := func(session acp.SessionId, value string) {
		t.Helper()
		options := modelOptions(t, `[{"id":"model","type":"select","currentValue":"`+value+`","options":[]}]`)
		if err := client.SessionUpdate(context.Background(), acp.SessionNotification{SessionId: session, Update: acp.SessionUpdate{ConfigOptionUpdate: &acp.SessionConfigOptionUpdate{ConfigOptions: options}}}); err != nil {
			t.Fatal(err)
		}
	}
	proc.trackSession("local", "remote-session")
	update("other-session", "wrong")
	update("remote-session", "first")
	update("remote-session", "first")
	update("remote-session", "second")
	update("remote-session", "")
	if len(events) != 2 {
		t.Fatalf("got %d events, want 2: %+v", len(events), events)
	}
	for i, want := range []string{"first", "second"} {
		var payload agentkit.MessageUpdatePayload
		if err := json.Unmarshal(events[i].Data, &payload); err != nil {
			t.Fatal(err)
		}
		if payload.AssistantMessageEvent.Type != agentkit.AssistantEventThinkingDelta || payload.AssistantMessageEvent.Delta != "[model: "+want+"]\n" {
			t.Fatalf("event = %+v, payload = %+v", events[i], payload)
		}
	}
	b.clearTurn()
	update("remote-session", "idle-model")
	if got, _ := proc.models.Load(acp.SessionId("remote-session")); got != "idle-model" {
		t.Fatalf("idle model = %v", got)
	}
}

// Exercise session/new and a second prompt on the same subprocess, including
// an agent that supplies no model information.
func TestRunTurnReportsRemoteModel(t *testing.T) {
	for _, mode := range []string{"model", "unsupported"} {
		t.Run(mode, func(t *testing.T) {
			exe, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			dir := t.TempDir()
			ag, err := New(Config{ID: "acp", Command: []string{exe, "-test.run=^TestACPSteeringHelper$"}, Cwd: dir, Env: map[string]string{"AGENTKIT_STEERING_TEST": mode}}, Deps{Workspace: &stubWorkspace{}, FS: bindTestFS(t, dir), Telemetry: mustToolkit(t)})
			if err != nil {
				t.Fatal(err)
			}
			defer ag.(*Runtime).bridgeFor("model-session").releaseSubprocess()
			ctx, cancel := context.WithTimeout(agenttest.TurnContext("model-session", "acp"), 5*time.Second)
			defer cancel()
			for turn := 0; turn < 2; turn++ {
				var models []string
				err := ag.RunTurn(ctx, agentkit.TurnInput{
					Message: agentkit.ModelMessage{Role: "user", Content: []agentkit.ContentPart{{Type: "text", Text: "hello"}}},
					Emit: func(_ context.Context, event agentkit.OutboundEvent) error {
						if event.Type == agentkit.EventMessageUpdate {
							var payload agentkit.MessageUpdatePayload
							if err := json.Unmarshal(event.Data, &payload); err != nil {
								return err
							}
							if payload.AssistantMessageEvent.Type == agentkit.AssistantEventThinkingDelta {
								models = append(models, payload.AssistantMessageEvent.Delta)
							}
						}
						return nil
					},
				})
				if err != nil {
					t.Fatal(err)
				}
				if mode == "model" {
					if len(models) != 1 || models[0] != "[model: remote-model]\n" {
						t.Fatalf("turn %d: models = %v", turn, models)
					}
				} else if len(models) != 0 {
					t.Fatalf("unknown model emitted: %v", models)
				}
			}
		})
	}
}

type modelRestoreStub struct {
	options []acp.SessionConfigOption
	err     error
}

func (s modelRestoreStub) LoadSession(context.Context, acp.LoadSessionRequest) (acp.LoadSessionResponse, error) {
	return acp.LoadSessionResponse{ConfigOptions: s.options}, s.err
}
func (s modelRestoreStub) ResumeSession(context.Context, acp.ResumeSessionRequest) (acp.ResumeSessionResponse, error) {
	return acp.ResumeSessionResponse{ConfigOptions: s.options}, s.err
}

func TestRestoredModel(t *testing.T) {
	options := modelOptions(t, `[{"id":"model","type":"select","currentValue":"restored-model","options":[]}]`)
	for _, method := range []string{"load", "resume"} {
		t.Run(method, func(t *testing.T) {
			proc := &subprocess{}
			restore := func(stub modelRestoreStub) error {
				conn := modelRestoreConn{SessionRestoreConn: stub, proc: proc}
				if method == "load" {
					_, err := conn.LoadSession(context.Background(), acp.LoadSessionRequest{SessionId: "restored"})
					return err
				}
				_, err := conn.ResumeSession(context.Background(), acp.ResumeSessionRequest{SessionId: "restored"})
				return err
			}
			if err := restore(modelRestoreStub{options: options}); err != nil {
				t.Fatal(err)
			}
			if model, _ := proc.models.Load(acp.SessionId("restored")); model != "restored-model" {
				t.Fatalf("model = %v", model)
			}
			if err := restore(modelRestoreStub{}); err != nil {
				t.Fatal(err)
			}
			if model, _ := proc.models.Load(acp.SessionId("restored")); model != "" {
				t.Fatalf("missing response model retained: %v", model)
			}
		})
	}
}
