package acpremote

import (
	"context"
	"encoding/json"
	"testing"

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

func TestConfigNotificationDoesNotEmitModel(t *testing.T) {
	b := &bridge{}
	proc := &subprocess{}
	b.proc.Store(proc)
	var events []agentkit.OutboundEvent
	e := newUpdateEmitter(mustToolkit(t), context.Background(), "local", "remote", func(_ context.Context, ev agentkit.OutboundEvent) error {
		events = append(events, ev)
		return nil
	}, agentkit.ModelMessage{})
	b.setTurn(turnState{emitter: e, sessionID: "local"})
	client := &bridgeClient{bridge: b}
	proc.client = client
	proc.trackSession("local", "remote-session")
	update := func(session acp.SessionId, value string) {
		t.Helper()
		options := modelOptions(t, `[{"id":"model","type":"select","currentValue":"`+value+`","options":[]}]`)
		if err := client.SessionUpdate(context.Background(), acp.SessionNotification{SessionId: session, Update: acp.SessionUpdate{ConfigOptionUpdate: &acp.SessionConfigOptionUpdate{ConfigOptions: options}}}); err != nil {
			t.Fatal(err)
		}
	}
	update("remote-session", "first")
	update("other-session", "wrong")
	options, _ := proc.configSnapshot()
	if got := modelFromConfig(options); got != "first" {
		t.Fatalf("config = %q, want first", got)
	}
	if len(events) != 0 || !e.firstTextAt.IsZero() {
		t.Fatal("configuration notification emitted generated content")
	}
	b.clearTurn()
	update("remote-session", "idle-model")
	options, _ = proc.configSnapshot()
	if got := modelFromConfig(options); got != "idle-model" {
		t.Fatalf("idle config = %q", got)
	}
	stale := &bridgeClient{bridge: b}
	if err := stale.SessionUpdate(context.Background(), acp.SessionNotification{SessionId: "remote-session", Update: acp.SessionUpdate{ConfigOptionUpdate: &acp.SessionConfigOptionUpdate{ConfigOptions: nil}}}); err != nil {
		t.Fatal(err)
	}
	options, _ = proc.configSnapshot()
	if got := modelFromConfig(options); got != "idle-model" {
		t.Fatalf("stale connection overwrote config: %q", got)
	}
}
