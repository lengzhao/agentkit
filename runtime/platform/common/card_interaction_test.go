package common

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/lengzhao/agentkit/cap/interaction"
)

type interactionHandlerFunc func(context.Context, interaction.Event) (bool, error)

func (f interactionHandlerFunc) HandleInteraction(ctx context.Context, e interaction.Event) (bool, error) {
	return f(ctx, e)
}

func TestInteractionContextScopedOptions(t *testing.T) {
	e := interaction.Event{Platform: "feishu", MessageID: "m1", UserID: "u1", CardStatus: interaction.CardAvailable,
		RawCard: json.RawMessage(`{"header":{"title":{"content":"部署问题"}},"body":{"elements":[{"tag":"markdown","content":"请选择环境"},{"name":"env","options":[{"value":"1","text":{"content":"生产"}}]},{"name":"region","options":[{"value":"1","text":{"content":"华东"}}]}]}}`),
		Answers: []interaction.Answer{{Control: "env", Value: "1"}, {Control: "region", Value: "1"}, {Control: "other", Value: "missing"}}, RawCallback: json.RawMessage(`{"event":{"answer":"1"}}`)}
	EnrichInteractionCard(&e)
	if e.Answers[0].Label != "生产" || e.Answers[1].Label != "华东" || e.Answers[2].Label != "" {
		t.Fatalf("wrong mapping: %+v", e.Answers)
	}
	got := FormatInteraction(e)
	for _, want := range []string{"部署问题", "请选择环境", "生产", "华东", "missing", "m1", "raw_card", "raw_callback"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %s: %s", want, got)
		}
	}
}
func TestInteractionFetchFailurePreservesAnswer(t *testing.T) {
	e := interaction.Event{MessageID: "m1", CardStatus: interaction.CardFailed, CardError: "access denied", Answers: []interaction.Answer{{Value: "my answer"}}}
	got := FormatInteraction(e)
	for _, s := range []string{"failed", "access denied", "my answer", "m1"} {
		if !strings.Contains(got, s) {
			t.Error(got)
		}
	}
	if strings.Contains(got, "(空)") {
		t.Error(got)
	}
}
func TestInteractionKeyIncludesOperationAndActor(t *testing.T) {
	base := interaction.Event{Platform: "p", ChatID: "c", MessageID: "m", UserID: "u", RawAction: json.RawMessage(`{"input":"a","options":["1"]}`)}
	k := InteractionEventKey(base)
	same := base
	same.RawAction = json.RawMessage(`{"options":["1"],"input":"a"}`)
	if k != InteractionEventKey(same) {
		t.Fatal("map ordering affected key")
	}
	for _, raw := range []string{`{"input":"b","options":["1"]}`, `{"input":"a","options":["2"]}`, `{"checked":false}`, `{"form":{"x":"a"}}`} {
		e := base
		e.RawAction = json.RawMessage(raw)
		if k == InteractionEventKey(e) {
			t.Fatal("different answer deduped")
		}
	}
	base.UserID = "v"
	if k == InteractionEventKey(base) {
		t.Fatal("different actor deduped")
	}
}
func TestInteractionHandlerChain(t *testing.T) {
	for _, mode := range []string{"", "forward", "ignore"} {
		calls := 0
		forwarded := false
		handlers := []interaction.Handler{
			interactionHandlerFunc(func(context.Context, interaction.Event) (bool, error) {
				calls++
				return false, errors.New("broken plugin")
			}),
			interactionHandlerFunc(func(context.Context, interaction.Event) (bool, error) { calls++; return false, nil }),
		}
		ProcessInteraction(context.Background(), interaction.Event{Platform: "test"}, InteractionProcessing{Mode: mode, Handlers: handlers, Forward: func(context.Context, interaction.Event) error { forwarded = true; return nil }})
		if calls != 2 || forwarded != (mode != "ignore") {
			t.Fatalf("mode=%s calls=%d forward=%v", mode, calls, forwarded)
		}
	}
	forwarded := false
	ProcessInteraction(context.Background(), interaction.Event{}, InteractionProcessing{Handlers: []interaction.Handler{interactionHandlerFunc(func(context.Context, interaction.Event) (bool, error) { return true, nil })}, Forward: func(context.Context, interaction.Event) error { forwarded = true; return nil }})
	if forwarded {
		t.Fatal("consumed event forwarded")
	}
}

func TestInteractionBuiltinConsumesOnlyWhenHandled(t *testing.T) {
	for _, handled := range []bool{true, false} {
		pluginCalled, forwarded := false, false
		ProcessInteraction(context.Background(), interaction.Event{}, InteractionProcessing{Builtin: func(context.Context, interaction.Event) bool { return handled }, Handlers: []interaction.Handler{interactionHandlerFunc(func(context.Context, interaction.Event) (bool, error) { pluginCalled = true; return false, nil })}, Forward: func(context.Context, interaction.Event) error { forwarded = true; return nil }})
		if pluginCalled == handled || forwarded == handled {
			t.Fatalf("handled=%v plugin=%v forward=%v", handled, pluginCalled, forwarded)
		}
	}
}

func TestInteractionKeyPreservesLargeNumbers(t *testing.T) {
	a := interaction.Event{RawAction: json.RawMessage(`{"answer":9007199254740992}`)}
	b := a
	b.RawAction = json.RawMessage(`{"answer":9007199254740993}`)
	if InteractionEventKey(a) == InteractionEventKey(b) {
		t.Fatal("distinct large integer answers collide")
	}
}

func TestInteractionDeliverySurvivesWorkerDeadline(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := ProcessInteraction(ctx, interaction.Event{}, InteractionProcessing{Forward: func(deliveryCtx context.Context, e interaction.Event) error {
		if deliveryCtx.Err() != nil {
			t.Fatal("expired worker canceled final delivery")
		}
		if _, ok := deliveryCtx.Deadline(); !ok {
			t.Fatal("delivery must retain its own time budget")
		}
		return nil
	}})
	if err != nil {
		t.Fatal(err)
	}
}
func TestInteractionReportsDeliveryFailure(t *testing.T) {
	want := errors.New("queue unavailable")
	err := ProcessInteraction(context.Background(), interaction.Event{}, InteractionProcessing{Forward: func(context.Context, interaction.Event) error { return want }})
	if !errors.Is(err, want) {
		t.Fatalf("delivery error lost: %v", err)
	}
}

func TestInteractionEventIDSeparatesIdenticalAnswers(t *testing.T) {
	a := interaction.Event{EventID: "one", RawAction: json.RawMessage(`{"option":"A"}`), RawCallback: json.RawMessage(`{"token":"first"}`)}
	replay := a
	replay.RawCallback = json.RawMessage(`{"token":"retry"}`)
	if InteractionEventKey(a) != InteractionEventKey(replay) {
		t.Fatal("delivery metadata changed event identity")
	}
	b := a
	b.EventID = "two"
	if InteractionEventKey(a) == InteractionEventKey(b) {
		t.Fatal("independent events with the same answer collided")
	}
}
