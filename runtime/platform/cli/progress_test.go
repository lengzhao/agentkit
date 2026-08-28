package cli

import (
	"bytes"
	"context"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/lengzhao/agentkit"
)

func TestFormatTurnProgress(t *testing.T) {
	t.Parallel()

	if got := formatTurnStart(); got != "\n[⏳ turn started]\n" {
		t.Fatalf("formatTurnStart() = %q", got)
	}
	if got := formatTurnEnd(1); got != "\n[✓ turn done · 1 step(s)]\n" {
		t.Fatalf("formatTurnEnd(1) = %q", got)
	}
	if got := formatTurnEnd(3); got != "\n[✓ turn done · 3 step(s)]\n" {
		t.Fatalf("formatTurnEnd(3) = %q", got)
	}
}

func TestCLITurnLifecycleOnStderr(t *testing.T) {

	p, err := New(Config{}, Deps{})
	if err != nil {
		t.Fatal(err)
	}
	platform := p.(*Platform)

	stderr, restore := captureStderr(t)

	ctx := context.Background()
	if err := platform.Send(ctx, agentkit.OutboundEvent{
		Type: agentkit.EventTurnStart,
		Data: agentkit.MarshalOutboundData(struct{}{}),
	}); err != nil {
		restore()
		t.Fatal(err)
	}
	if err := platform.Send(ctx, agentkit.OutboundEvent{
		Type: agentkit.EventTurnEnd,
		Data: agentkit.MarshalOutboundData(struct {
			Steps int `json:"steps"`
		}{Steps: 2}),
	}); err != nil {
		restore()
		t.Fatal(err)
	}
	restore()

	got := stderr.String()
	for _, want := range []string{"⏳ turn started", "✓ turn done · 2 step(s)"} {
		if !strings.Contains(got, want) {
			t.Fatalf("stderr missing %q:\n%s", want, got)
		}
	}
}

func TestCLISendKeepsInternalEventsOffStdout(t *testing.T) {

	p, err := New(Config{}, Deps{})
	if err != nil {
		t.Fatal(err)
	}
	platform := p.(*Platform)

	stdout, restoreOut := captureStdout(t)
	stderr, restoreErr := captureStderr(t)

	ctx := context.Background()
	events := []agentkit.OutboundEvent{
		{Type: agentkit.EventTurnStart, Data: []byte(`{}`)},
		{Type: agentkit.EventTurnEnd, Data: []byte(`{"steps":1}`)},
		{Type: agentkit.EventStepStart, Data: []byte(`{"step":0}`)},
	}
	for _, event := range events {
		if err := platform.Send(ctx, event); err != nil {
			restoreOut()
			restoreErr()
			t.Fatal(err)
		}
	}
	restoreOut()
	restoreErr()

	if stdout.Len() != 0 {
		t.Fatalf("stdout must stay clean, got %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "⏳ turn started") {
		t.Fatalf("stderr missing turn start marker:\n%s", stderr.String())
	}
}

func captureStderr(t *testing.T) (*bytes.Buffer, func()) {
	t.Helper()
	old := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stderr = w
	buf := &bytes.Buffer{}
	done := make(chan struct{})
	go func() {
		_, _ = io.Copy(buf, r)
		close(done)
	}()
	return buf, func() {
		os.Stderr = old
		_ = w.Close()
		<-done
	}
}

func captureStdout(t *testing.T) (*bytes.Buffer, func()) {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	buf := &bytes.Buffer{}
	done := make(chan struct{})
	go func() {
		_, _ = io.Copy(buf, r)
		close(done)
	}()
	return buf, func() {
		os.Stdout = old
		_ = w.Close()
		<-done
	}
}
