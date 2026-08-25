package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/lengzhao/agentkit/cap/interaction"
)

func (p *Platform) ReadReply(ctx context.Context, req interaction.Human) (interaction.Reply, error) {
	if p.input == nil {
		p.input = NewInput(os.Stdin)
	}
	line, err := p.input.ReadAnswer(ctx)
	if err != nil {
		if errors.Is(err, io.EOF) {
			fmt.Fprintln(os.Stderr)
			return interaction.Reply{}, fmt.Errorf("stdin is closed, no interactive user")
		}
		return interaction.Reply{}, err
	}
	return interaction.Reply{Text: line}, nil
}

func renderInteractionStart(payload interaction.StartPayload) {
	prefix := "[agent asks]"
	switch payload.Kind {
	case interaction.Approval:
		prefix = "[approval needed]"
	case interaction.Confirmation:
		prefix = "[confirm]"
	}
	fmt.Fprintf(os.Stderr, "\n%s %s\n", prefix, payload.Prompt)
	for i, opt := range payload.Options {
		label := strings.TrimSpace(opt.Label)
		if label == "" {
			continue
		}
		fmt.Fprintf(os.Stderr, "  %d) %s\n", i+1, label)
	}
	if payload.Default != "" {
		fmt.Fprintf(os.Stderr, "answer [%s]: ", payload.Default)
	} else {
		fmt.Fprint(os.Stderr, "answer: ")
	}
}

var _ interaction.Handler = (*Platform)(nil)
