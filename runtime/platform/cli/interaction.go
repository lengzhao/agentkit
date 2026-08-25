package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/lengzhao/agentkit"
)

func (p *Platform) ReadInteractionReply(ctx context.Context, req agentkit.HumanInteraction) (agentkit.InteractionReply, error) {
	if p.input == nil {
		p.input = NewInput(os.Stdin)
	}
	line, err := p.input.ReadAnswer(ctx)
	if err != nil {
		if errors.Is(err, io.EOF) {
			fmt.Fprintln(os.Stderr)
			return agentkit.InteractionReply{}, fmt.Errorf("stdin is closed, no interactive user")
		}
		return agentkit.InteractionReply{}, err
	}
	return agentkit.InteractionReply{Text: line}, nil
}

func renderInteractionStart(payload agentkit.InteractionStartPayload) {
	prefix := "[agent asks]"
	switch payload.Kind {
	case agentkit.InteractionApproval:
		prefix = "[approval needed]"
	case agentkit.InteractionConfirmation:
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

var _ agentkit.InteractionHandler = (*Platform)(nil)
