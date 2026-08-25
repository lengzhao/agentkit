package tool

import (
	"context"
	"fmt"
	"strings"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/ask"
)

type AskUserConfig struct{}

type AskUserDeps struct {
	Ask ask.Service `json:"ask"`
}

type AskUserInput struct {
	Question string   `json:"question" jsonschema:"required,description=One specific question whose answer changes what you do next"`
	Options  []string `json:"options,omitempty" jsonschema:"description=Offer concrete choices when the answer is one of a few known options"`
	Default  string   `json:"default,omitempty" jsonschema:"description=Answer to assume when the user just presses enter"`
}

type AskUserOutput struct {
	// Answered is false when nobody was available. The question was not
	// answered, and asking again will not help.
	Answered bool   `json:"answered"`
	Answer   string `json:"answer,omitempty"`
	// Selected indexes Options, or -1 for a free-form answer.
	Selected int    `json:"selected"`
	Reason   string `json:"reason,omitempty"`
	// Guidance tells the model what to do with an unanswered question. It is
	// part of the result rather than the description so it lands right where the
	// model reads the refusal.
	Guidance string `json:"guidance,omitempty"`
}

// NewAskUser builds the tool that lets an agent put a question to the human.
//
// An unattended run gets Answered=false rather than an error or a block: the
// deps.ask provider decides (ask/cli reads stdin, ask/unavailable always
// declines), and either way the turn continues.
func NewAskUser(_ AskUserConfig, deps AskUserDeps) (agentkit.Tool, error) {
	if deps.Ask == nil {
		return nil, fmt.Errorf("tool/ask-user requires ask dependency")
	}
	return agentkit.NewTool("ask_user", func(ctx context.Context, input AskUserInput) (AskUserOutput, error) {
		question := strings.TrimSpace(input.Question)
		if question == "" {
			return AskUserOutput{}, fmt.Errorf("question is required")
		}
		answer, err := deps.Ask.Ask(ctx, ask.Question{
			Question: question,
			Options:  input.Options,
			Default:  strings.TrimSpace(input.Default),
		})
		if err != nil {
			return AskUserOutput{}, err
		}
		out := AskUserOutput{
			Answered: answer.Answered,
			Answer:   answer.Text,
			Selected: answer.Selected,
			Reason:   answer.Reason,
		}
		if !answer.Answered {
			out.Guidance = "Nobody answered. Do not ask again: pick the most reasonable option yourself, state the assumption you made, and continue."
		}
		return out, nil
	}).Description("Ask the user one question when their answer changes what you do next. Do not use it for questions you can answer by reading the workspace. If nobody is available the result says so, and you should proceed on your own judgement.").Build()
}
