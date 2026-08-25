package ask

import (
	"context"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	capask "github.com/lengzhao/agentkit/cap/ask"
)

// newCLI builds a provider reading from the given input instead of os.Stdin, so
// these tests touch no process globals and can run in parallel. strings.Reader
// returns EOF at the end, which is exactly what a closed terminal does.
func newCLI(t *testing.T, input string) *CLI {
	t.Helper()
	svc, err := NewCLI(CLIConfig{})
	if err != nil {
		t.Fatal(err)
	}
	cli := svc.(*CLI)
	cli.in = strings.NewReader(input)
	cli.out = io.Discard
	return cli
}

func TestCLIFreeFormAnswer(t *testing.T) {
	t.Parallel()

	got, err := newCLI(t, "use sqlite\n").Ask(context.Background(), capask.Question{Question: "which store?"})
	if err != nil {
		t.Fatal(err)
	}
	if !got.Answered || got.Text != "use sqlite" || got.Selected != -1 {
		t.Errorf("answer = %+v", got)
	}
}

func TestCLIOptionByIndexAndByText(t *testing.T) {
	t.Parallel()

	got, err := newCLI(t, "2\n").Ask(context.Background(), capask.Question{
		Question: "which provider?",
		Options:  []string{"exa", "scripted"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !got.Answered || got.Text != "scripted" || got.Selected != 1 {
		t.Errorf("answer by index = %+v", got)
	}

	got, err = newCLI(t, "EXA\n").Ask(context.Background(), capask.Question{
		Question: "which provider?",
		Options:  []string{"exa", "scripted"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !got.Answered || got.Text != "exa" || got.Selected != 0 {
		t.Errorf("answer by text = %+v", got)
	}
}

func TestCLIOffMenuAnswerIsKept(t *testing.T) {
	t.Parallel()

	got, err := newCLI(t, "neither, use postgres\n").Ask(context.Background(), capask.Question{
		Question: "which store?",
		Options:  []string{"sqlite", "jsonl"},
	})
	if err != nil {
		t.Fatal(err)
	}
	// The human is allowed to say something that is not on the menu.
	if !got.Answered || got.Text != "neither, use postgres" || got.Selected != -1 {
		t.Errorf("answer = %+v", got)
	}
}

func TestCLIEmptyLineUsesDefault(t *testing.T) {
	t.Parallel()

	got, err := newCLI(t, "\n").Ask(context.Background(), capask.Question{Question: "proceed?", Default: "yes"})
	if err != nil {
		t.Fatal(err)
	}
	if !got.Answered || got.Text != "yes" {
		t.Errorf("answer = %+v", got)
	}

	got, err = newCLI(t, "\n").Ask(context.Background(), capask.Question{Question: "proceed?"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Answered || got.Reason == "" {
		t.Errorf("empty answer without a default should be unanswered: %+v", got)
	}
}

func TestCLIClosedStdinDegradesInsteadOfFailing(t *testing.T) {
	t.Parallel()

	got, err := newCLI(t, "").Ask(context.Background(), capask.Question{Question: "anyone there?"})
	if err != nil {
		t.Fatalf("a closed stdin is the normal state under cron, not an error: %v", err)
	}
	if got.Answered || !strings.Contains(got.Reason, "stdin") {
		t.Errorf("answer = %+v", got)
	}
}

func TestCLIRespectsContextCancellation(t *testing.T) {
	t.Parallel()

	// A pipe nobody writes to: the read blocks until ctx expires.
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		w.Close()
		r.Close()
	})
	cli := newCLI(t, "")
	cli.in = r

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	done := make(chan capask.Answer, 1)
	go func() {
		got, err := cli.Ask(ctx, capask.Question{Question: "waiting?"})
		if err != nil {
			t.Errorf("cancellation must not be an error: %v", err)
		}
		done <- got
	}()

	select {
	case got := <-done:
		if got.Answered || got.Reason == "" {
			t.Errorf("answer = %+v, want abandoned", got)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Ask ignored the context deadline")
	}
}

func TestCLIDiscardsAnswerTypedAfterTheQuestionWasAbandoned(t *testing.T) {
	t.Parallel()

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { w.Close(); r.Close() })
	cli := newCLI(t, "")
	cli.in = r

	// First question times out with nothing typed.
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	got, err := cli.Ask(ctx, capask.Question{Question: "still there?"})
	if err != nil || got.Answered {
		t.Fatalf("first question should be abandoned: %+v err=%v", got, err)
	}

	// The human types only now — an answer to a question nobody is waiting on.
	if _, err := io.WriteString(w, "too late\n"); err != nil {
		t.Fatal(err)
	}
	// Give the reader goroutine time to park on the stale line.
	time.Sleep(50 * time.Millisecond)

	// The second question must not silently adopt it.
	ctx2, cancel2 := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel2()
	got, err = cli.Ask(ctx2, capask.Question{Question: "different question?"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Answered {
		t.Errorf("stale line was used as the answer to a later question: %+v", got)
	}
}

func TestCLIRequiresQuestion(t *testing.T) {
	t.Parallel()

	if _, err := newCLI(t, "").Ask(context.Background(), capask.Question{}); err == nil {
		t.Fatal("empty question was accepted")
	}
}

func TestUnavailableAlwaysDeclines(t *testing.T) {
	t.Parallel()

	svc, err := NewUnavailable(UnavailableConfig{})
	if err != nil {
		t.Fatal(err)
	}
	got, err := svc.Ask(context.Background(), capask.Question{Question: "anything"})
	if err != nil {
		t.Fatalf("declining is not an error: %v", err)
	}
	if got.Answered || got.Reason == "" || got.Selected != -1 {
		t.Errorf("answer = %+v", got)
	}

	svc, err = NewUnavailable(UnavailableConfig{Reason: "cron run, decide yourself"})
	if err != nil {
		t.Fatal(err)
	}
	got, _ = svc.Ask(context.Background(), capask.Question{Question: "anything"})
	if got.Reason != "cron run, decide yourself" {
		t.Errorf("reason = %q, want the configured text", got.Reason)
	}
}
