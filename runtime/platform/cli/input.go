package cli

import (
	"bufio"
	"context"
	"errors"
	"io"
	"os"
)

// Input owns one stdin stream for prompt lines and permission replies.
type Input struct {
	in io.Reader
	br *bufio.Reader
}

func NewInput(in io.Reader) *Input {
	if in == nil {
		in = os.Stdin
	}
	return &Input{in: in}
}

// ReadPrompt reads one line from stdin.
func (t *Input) ReadPrompt() (string, error) {
	return t.ReadPromptContext(context.Background())
}

// ReadPromptContext reads one line until newline, EOF, or ctx cancellation.
func (t *Input) ReadPromptContext(ctx context.Context) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	type lineResult struct {
		line string
		err  error
	}
	ch := make(chan lineResult, 1)
	go func() {
		if t.br == nil {
			t.br = bufio.NewReader(t.in)
		}
		line, err := t.br.ReadString('\n')
		if errors.Is(err, io.EOF) && len(line) > 0 {
			ch <- lineResult{line: trimLine(line)}
			return
		}
		if err != nil {
			ch <- lineResult{err: err}
			return
		}
		ch <- lineResult{line: trimLine(line)}
	}()
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case res := <-ch:
		return res.line, res.err
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
