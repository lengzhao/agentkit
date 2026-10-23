package common

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lengzhao/agentkit/cap/interaction"
)

func submissionEvent() interaction.Event {
	return interaction.Event{Platform: "p", ChatID: "c", MessageID: "card", UserID: "u", Purpose: interaction.PurposeSubmit, Answers: []interaction.Answer{{Control: "answer", Value: "A"}}}
}
func TestCardSubmissionConcurrentClicks(t *testing.T) {
	var gate CardSubmissions
	var forwards, closes atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := ProcessInteraction(context.Background(), submissionEvent(), InteractionProcessing{Submissions: &gate, Forward: func(context.Context, interaction.Event) error { forwards.Add(1); return nil }, CloseCard: func(context.Context, interaction.Event, string) error { closes.Add(1); return nil }})
			if err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if forwards.Load() != 1 || closes.Load() != 1 {
		t.Fatalf("forward=%d close=%d", forwards.Load(), closes.Load())
	}
}
func TestCardSubmissionDeliveryFailureCanRetry(t *testing.T) {
	var gate CardSubmissions
	calls := 0
	closed := 0
	p := InteractionProcessing{Submissions: &gate, Forward: func(context.Context, interaction.Event) error {
		calls++
		if calls == 1 {
			return errors.New("queue failed")
		}
		return nil
	}, CloseCard: func(context.Context, interaction.Event, string) error { closed++; return nil }}
	if err := ProcessInteraction(context.Background(), submissionEvent(), p); err == nil {
		t.Fatal("missing delivery failure")
	}
	if err := ProcessInteraction(context.Background(), submissionEvent(), p); err != nil {
		t.Fatal(err)
	}
	_ = ProcessInteraction(context.Background(), submissionEvent(), p)
	if calls != 2 || closed != 1 {
		t.Fatalf("calls=%d closed=%d", calls, closed)
	}
}
func TestCardUpdateFailureDoesNotUnlockSubmission(t *testing.T) {
	var gate CardSubmissions
	calls := 0
	p := InteractionProcessing{Submissions: &gate, Forward: func(context.Context, interaction.Event) error { calls++; return nil }, CloseCard: func(context.Context, interaction.Event, string) error { return errors.New("patch failed") }}
	if err := ProcessInteraction(context.Background(), submissionEvent(), p); err != nil {
		t.Fatal(err)
	}
	_ = ProcessInteraction(context.Background(), submissionEvent(), p)
	if calls != 1 {
		t.Fatal("update failure re-executed submission")
	}
}
func TestPluginOwnsCardLifecycle(t *testing.T) {
	for _, closeIt := range []bool{false, true} {
		var gate CardSubmissions
		calls, closed, forwarded := 0, 0, 0
		h := interactionHandlerFunc(func(ctx context.Context, e interaction.Event) (bool, error) {
			calls++
			if closeIt {
				if err := interaction.CloseCard(ctx, "plugin answer"); err != nil {
					t.Error(err)
				}
			}
			return true, nil
		})
		p := InteractionProcessing{Submissions: &gate, Handlers: []interaction.Handler{h}, Forward: func(context.Context, interaction.Event) error { forwarded++; return nil }, CloseCard: func(_ context.Context, _ interaction.Event, text string) error {
			if text != "plugin answer" {
				t.Error(text)
			}
			closed++
			return nil
		}}
		for i := 0; i < 2; i++ {
			_ = ProcessInteraction(context.Background(), submissionEvent(), p)
		}
		if calls != 2 || forwarded != 0 || closed != map[bool]int{false: 0, true: 2}[closeIt] || gate.Contains(submissionEvent()) {
			t.Fatalf("plugin lifecycle changed: calls=%d close=%d forwarded=%d", calls, closed, forwarded)
		}
	}
}
func TestSubmissionDraftsAndNavigation(t *testing.T) {
	var gate CardSubmissions
	calls, closed := 0, 0
	p := InteractionProcessing{Submissions: &gate, Forward: func(context.Context, interaction.Event) error { calls++; return nil }, CloseCard: func(context.Context, interaction.Event, string) error { closed++; return nil }}
	e := submissionEvent()
	e.Purpose = interaction.PurposeChange
	_ = ProcessInteraction(context.Background(), e, p)
	if calls != 0 || gate.Contains(e) {
		t.Fatal("draft was submitted")
	}
	e.Purpose = interaction.PurposeNavigate
	for i := 0; i < 2; i++ {
		_ = ProcessInteraction(context.Background(), e, p)
	}
	if calls != 2 || closed != 0 {
		t.Fatal("navigation became one-shot")
	}
}
func TestPerUserSubmissionKeepsSharedCardOpen(t *testing.T) {
	var gate CardSubmissions
	calls, closed, receipts := 0, 0, 0
	p := InteractionProcessing{Submissions: &gate, Forward: func(context.Context, interaction.Event) error { calls++; return nil }, CloseCard: func(context.Context, interaction.Event, string) error { closed++; return nil }, NotifyUser: func(context.Context, interaction.Event, string) error { receipts++; return nil }}
	e := submissionEvent()
	e.SubmissionMode = interaction.SubmissionPerUser
	for _, user := range []string{"u1", "u1", "u2", "u2"} {
		e.UserID = user
		_ = ProcessInteraction(context.Background(), e, p)
	}
	if calls != 2 || closed != 0 || receipts != 4 {
		t.Fatalf("calls=%d closed=%d receipts=%d", calls, closed, receipts)
	}
}
func TestSubmissionTTLCleansCompletedButNotPending(t *testing.T) {
	var gate CardSubmissions
	e := submissionEvent()
	done, duplicate := gate.Begin(e, 20*time.Millisecond)
	if duplicate {
		t.Fatal("new card duplicate")
	}
	time.Sleep(30 * time.Millisecond)
	if _, duplicate = gate.Begin(e, 20*time.Millisecond); !duplicate {
		t.Fatal("pending submission expired")
	}
	done(nil)
	deadline := time.Now().Add(time.Second)
	for gate.Contains(e) && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if gate.Contains(e) {
		t.Fatal("completed entry not cleaned")
	}
	if _, duplicate = gate.Begin(e, 20*time.Millisecond); duplicate {
		t.Fatal("expired card stayed locked")
	}
}

func TestUnhandledPluginStillPrecedesSubmissionGate(t *testing.T) {
	var gate CardSubmissions
	calls, forwards := 0, 0
	p := InteractionProcessing{Submissions: &gate, Handlers: []interaction.Handler{interactionHandlerFunc(func(context.Context, interaction.Event) (bool, error) { calls++; return false, nil })}, Forward: func(context.Context, interaction.Event) error { forwards++; return nil }}
	for i := 0; i < 2; i++ {
		_ = ProcessInteraction(context.Background(), submissionEvent(), p)
	}
	if calls != 2 || forwards != 1 {
		t.Fatalf("calls=%d forward=%d", calls, forwards)
	}
}
func TestRepeatableSubmissionDoesNotLockOrClose(t *testing.T) {
	var gate CardSubmissions
	calls, closed := 0, 0
	e := submissionEvent()
	e.SubmissionMode = interaction.SubmissionRepeat
	p := InteractionProcessing{Submissions: &gate, Forward: func(context.Context, interaction.Event) error { calls++; return nil }, CloseCard: func(context.Context, interaction.Event, string) error { closed++; return nil }}
	for i := 0; i < 2; i++ {
		_ = ProcessInteraction(context.Background(), e, p)
	}
	if calls != 2 || closed != 0 || gate.Contains(e) {
		t.Fatal("repeatable interaction was locked")
	}
}
func TestCardSubmissionTTLValidation(t *testing.T) {
	for _, s := range []string{"0s", "-1h", "invalid"} {
		if _, err := ParseCardSubmissionTTL(s); err == nil {
			t.Fatalf("accepted invalid duration %q", s)
		}
	}
	if ttl, err := ParseCardSubmissionTTL(""); err != nil || ttl != 24*time.Hour {
		t.Fatal("wrong default TTL")
	}
}

func TestSubmittedButtonAnswerKeepsSelectedLabel(t *testing.T) {
	e := interaction.Event{Action: "risk_level_test", RawCard: json.RawMessage(`{"elements":[{"tag":"button","text":{"content":"进取型"},"value":{"action":"risk_level_test","value":"high","test_id":"t"}}]}`), Answers: []interaction.Answer{{Value: "high"}}}
	EnrichInteractionCard(&e)
	text := SubmittedInteractionText(e)
	if !strings.Contains(text, "进取型 (high)") {
		t.Fatalf("lost selected label: %s", text)
	}
	e.Answers = []interaction.Answer{{Value: map[string]any{"action": "risk_level_test", "risk": "high"}}}
	if strings.Contains(SubmittedInteractionText(e), "risk_level_test") {
		t.Fatal("routing action shown as answer")
	}
}
