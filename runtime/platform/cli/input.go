package cli

import (
	"bufio"
	"errors"
	"io"
	"os"
)

// Input owns one stdin stream for prompt lines and permission replies.
type Input struct {
	in io.Reader
}

func NewInput(in io.Reader) *Input {
	if in == nil {
		in = os.Stdin
	}
	return &Input{in: in}
}

// ReadPrompt reads one line from stdin.
func (t *Input) ReadPrompt() (string, error) {
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
