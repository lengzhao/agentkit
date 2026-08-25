package ask

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/lengzhao/agentkit/cap/ask"
	"github.com/lengzhao/pluginkit"
)

type CLIConfig struct {
	// Prefix labels the question on the terminal.
	Prefix string `json:"prefix,omitempty"`
}

type CLI struct {
	prefix string

	// in / out default to os.Stdin / os.Stderr; tests inject a pipe instead of
	// swapping the process globals.
	in  io.Reader
	out io.Writer

	// One question at a time: two concurrent sessions sharing one terminal must
	// not interleave their prompts, and must not both read the same stdin.
	mu    sync.Mutex
	once  sync.Once
	lines chan lineResult
}

type lineResult struct {
	line string
	err  error
}

func init() {
	pluginkit.Register("ask/cli", NewCLI)
	pluginkit.Register("ask/unavailable", NewUnavailable)
}

func NewCLI(cfg CLIConfig) (ask.Service, error) {
	prefix := cfg.Prefix
	if prefix == "" {
		prefix = "[agent asks]"
	}
	return &CLI{prefix: prefix}, nil
}

func (c *CLI) Ask(ctx context.Context, q ask.Question) (ask.Answer, error) {
	question := strings.TrimSpace(q.Question)
	if question == "" {
		return ask.Answer{}, fmt.Errorf("question is required")
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	c.startReader()
	c.discardStale()

	out := c.writer()
	fmt.Fprintf(out, "\n%s %s\n", c.prefix, question)
	for i, opt := range q.Options {
		fmt.Fprintf(out, "  %d) %s\n", i+1, opt)
	}
	if q.Default != "" {
		fmt.Fprintf(out, "answer [%s]: ", q.Default)
	} else {
		fmt.Fprint(out, "answer: ")
	}

	var got lineResult
	select {
	case <-ctx.Done():
		// The reader goroutine stays parked on the unread line; the next
		// question discards it rather than treating it as its own answer.
		return ask.Answer{Selected: -1, Reason: "question abandoned: " + ctx.Err().Error()}, nil
	case got = <-c.lines:
	}

	if got.err != nil && !(errors.Is(got.err, io.EOF) && strings.TrimSpace(got.line) != "") {
		if errors.Is(got.err, io.EOF) {
			// A closed stdin is the normal state under cron / CI, not a failure:
			// report it as unanswered so the turn continues.
			fmt.Fprintln(out)
			return ask.Answer{Selected: -1, Reason: "stdin is closed, no interactive user"}, nil
		}
		return ask.Answer{}, got.err
	}

	text := strings.TrimSpace(got.line)
	if text == "" {
		if q.Default == "" {
			return ask.Answer{Selected: -1, Reason: "user gave an empty answer"}, nil
		}
		text = q.Default
	}
	return matchOption(text, q.Options), nil
}

// startReader spawns the single goroutine that owns the input stream. One
// goroutine for the process lifetime, not one per question: a read cannot be
// interrupted, so a question abandoned on timeout would otherwise leak a
// goroutine — and two of them would then race for the same stdin.
func (c *CLI) startReader() {
	c.once.Do(func() {
		c.lines = make(chan lineResult)
		in := c.in
		if in == nil {
			in = os.Stdin
		}
		lines := c.lines
		go func() {
			r := bufio.NewReader(in)
			for {
				line, err := r.ReadString('\n')
				res := lineResult{line: line, err: err}
				if err != nil {
					// A dead stdin stays dead: hand the same result to every
					// later question instead of re-reading a closed pipe.
					for {
						lines <- res
					}
				}
				lines <- res
			}
		}()
	})
}

// discardStale drops a line that arrived while nobody was waiting for it. Input
// typed before the question was printed is not an answer to that question.
func (c *CLI) discardStale() {
	for {
		select {
		case res := <-c.lines:
			if res.err != nil {
				// EOF is resent forever, so putting it back is not needed and
				// draining it would spin.
				return
			}
		default:
			return
		}
	}
}

func (c *CLI) writer() io.Writer {
	if c.out != nil {
		return c.out
	}
	return os.Stderr
}

// matchOption resolves the typed answer against Options, accepting either the
// 1-based index or the option text. Free-form questions keep Selected at -1.
func matchOption(text string, options []string) ask.Answer {
	if len(options) == 0 {
		return ask.Answer{Answered: true, Text: text, Selected: -1}
	}
	if n, err := strconv.Atoi(text); err == nil && n >= 1 && n <= len(options) {
		return ask.Answer{Answered: true, Text: options[n-1], Selected: n - 1}
	}
	for i, opt := range options {
		if strings.EqualFold(text, strings.TrimSpace(opt)) {
			return ask.Answer{Answered: true, Text: opt, Selected: i}
		}
	}
	// Off-menu answers are kept rather than rejected; the model asked for a
	// preference, and the human is allowed to say something else.
	return ask.Answer{Answered: true, Text: text, Selected: -1}
}
