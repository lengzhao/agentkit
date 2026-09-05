package cli

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/lengzhao/agentkit"
)

type stubStopCommands struct {
	called bool
}

func (stubStopCommands) Dispatch(ctx context.Context, name, _ string) (string, error) {
	if name == "stop" {
		return "stopping current turn", nil
	}
	return "", agentkit.ErrCommandNotHandled
}

func (stubStopCommands) List() []agentkit.Command { return nil }

func TestCLIStopDuringTurn(t *testing.T) {
	p, err := New(Config{}, Deps{Commands: stubStopCommands{}})
	if err != nil {
		t.Fatal(err)
	}
	platform := p.(*Platform)
	platform.welcomed = true
	platform.input = NewInput(strings.NewReader("/stop\n"))
	platform.beginTurnWait()

	done := make(chan error, 1)
	go func() {
		_, err := platform.Receive(context.Background())
		done <- err
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("Receive blocked while turn in progress")
	}
}

func TestCLINonSlashHeldUntilTurnEnd(t *testing.T) {
	p, err := New(Config{}, Deps{})
	if err != nil {
		t.Fatal(err)
	}
	platform := p.(*Platform)
	platform.welcomed = true
	platform.input = NewInput(strings.NewReader("hello\n"))
	platform.beginTurnWait()

	done := make(chan struct {
		event agentkit.MessageEvent
		err   error
	}, 1)
	go func() {
		ev, err := platform.Receive(context.Background())
		done <- struct {
			event agentkit.MessageEvent
			err   error
		}{ev, err}
	}()

	select {
	case <-done:
		t.Fatal("Receive returned before turn ended")
	case <-time.After(50 * time.Millisecond):
	}

	platform.endTurnWait()

	select {
	case res := <-done:
		if res.err != nil {
			t.Fatal(res.err)
		}
		if textOf(res.event.Message) != "hello" {
			t.Fatalf("message = %q, want hello", textOf(res.event.Message))
		}
	case <-time.After(time.Second):
		t.Fatal("Receive did not finish after turn end")
	}
}

var _ agentkit.Commands = stubStopCommands{}
