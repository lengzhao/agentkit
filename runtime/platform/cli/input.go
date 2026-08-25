package cli

import (
	"bufio"
	"context"
	"errors"
	"io"
	"os"
	"sync"
)

type lineResult struct {
	line string
	err  error
}

// Input owns one stdin stream for both prompt lines and HIL interaction answers.
// Only one read is active at a time so the `> ` loop and ask_user never race.
type Input struct {
	in io.Reader
	mu sync.Mutex
	// waiting/armed gate the background reader to a single interaction line.
	waiting bool
	armed   bool
	cond    *sync.Cond
	once    sync.Once
	lines   chan lineResult
}

func NewInput(in io.Reader) *Input {
	if in == nil {
		in = os.Stdin
	}
	t := &Input{in: in}
	t.cond = sync.NewCond(&t.mu)
	return t
}

// ReadPrompt reads one interactive prompt line (the `> ` loop).
func (t *Input) ReadPrompt() (string, error) {
	t.mu.Lock()
	if t.waiting {
		t.mu.Unlock()
		return "", errors.New("cannot read prompt while interaction is active")
	}
	t.mu.Unlock()

	r := bufio.NewReader(t.in)
	line, err := r.ReadString('\n')
	if err != nil {
		if errors.Is(err, io.EOF) && len(line) > 0 {
			return trimLine(line), nil
		}
		return "", err
	}
	return trimLine(line), nil
}

// ReadAnswer blocks until the user submits one answer line for a pending interaction.
func (t *Input) ReadAnswer(ctx context.Context) (string, error) {
	t.startReader()
	t.discardStale()

	t.mu.Lock()
	t.waiting = true
	t.armed = true
	t.cond.Signal()
	t.mu.Unlock()

	defer func() {
		t.mu.Lock()
		t.waiting = false
		t.armed = false
		t.cond.Broadcast()
		t.mu.Unlock()
	}()

	var got lineResult
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case got = <-t.lines:
	}

	if got.err != nil && !(errors.Is(got.err, io.EOF) && trimLine(got.line) != "") {
		return "", got.err
	}
	return trimLine(got.line), nil
}

func (t *Input) startReader() {
	t.once.Do(func() {
		t.lines = make(chan lineResult, 1)
		in := t.in
		lines := t.lines
		go func() {
			r := bufio.NewReader(in)
			for {
				t.mu.Lock()
				for !t.waiting || !t.armed {
					t.cond.Wait()
				}
				t.mu.Unlock()

				line, err := r.ReadString('\n')
				res := lineResult{line: line, err: err}

				t.mu.Lock()
				if !t.armed {
					t.mu.Unlock()
					continue
				}
				t.armed = false
				t.mu.Unlock()

				lines <- res
			}
		}()
	})
}

func (t *Input) discardStale() {
	for {
		select {
		case res := <-t.lines:
			if res.err != nil {
				return
			}
		default:
			return
		}
	}
}

func trimLine(line string) string {
	for len(line) > 0 && (line[len(line)-1] == '\n' || line[len(line)-1] == '\r') {
		line = line[:len(line)-1]
	}
	for len(line) > 0 && (line[0] == ' ' || line[0] == '\t') {
		line = line[1:]
	}
	for len(line) > 0 && (line[len(line)-1] == ' ' || line[len(line)-1] == '\t') {
		line = line[:len(line)-1]
	}
	return line
}
