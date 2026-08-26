package tool

import (
	"context"
	"fmt"
	"strings"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/interaction"
)

type AskUserConfig struct{}

type AskUserDeps struct{}

type AskUserInput struct {
	Question string   `json:"question" jsonschema:"required,description=One specific question whose answer changes what you do next"`
	Options  []string `json:"options,omitempty" jsonschema:"description=Offer concrete choices when the answer is one of a few known options"`
	Default  string   `json:"default,omitempty" jsonschema:"description=Answer to assume when the user just presses enter"`
}

type AskUserOutput struct {
	Answered bool   `json:"answered"`
	Answer   string `json:"answer,omitempty"`
	Selected int    `json:"selected"`
	Reason   string `json:"reason,omitempty"`
	Guidance string `json:"guidance,omitempty"`
}

// NewAskUser registers tool/ask-user: Ask the human one question (tool name: ask_user) whose answer changes what the agent does next.
//
// Best practices:
//   - Routes through the inbound platform via cap/interaction.Session; interactive CLI reads stdin, IM platforms render cards/buttons.
//   - Headless platforms return answered=false immediately; it is never an error and never blocks the turn.
//   - Do not mount it on subagents: a child agent runs behind a delegate call, where nobody is watching its stdout.
func NewAskUser(_ AskUserConfig, _ AskUserDeps) (agentkit.ToolPack, error) {
	tool, err := agentkit.NewTool("ask_user", func(ctx context.Context, input AskUserInput) (AskUserOutput, error) {
		question := strings.TrimSpace(input.Question)
		if question == "" {
			return AskUserOutput{}, fmt.Errorf("question is required")
		}

		si, ok := ctx.Value(agentkit.KeySessionControl).(interaction.Session)
		if !ok || si == nil {
			return unansweredAsk("session interaction unavailable"), nil
		}

		options := make([]interaction.Option, 0, len(input.Options))
		for _, opt := range input.Options {
			options = append(options, interaction.Option{Label: opt})
		}

		result, err := si.Run(ctx, interaction.Human{
			Kind:    interaction.Question,
			Prompt:  question,
			Options: options,
			Default: strings.TrimSpace(input.Default),
		})
		if err != nil {
			return AskUserOutput{}, err
		}

		out := AskUserOutput{
			Answered: result.Answered,
			Answer:   result.Text,
			Selected: result.Selected,
			Reason:   result.Reason,
		}
		if !result.Answered {
			out.Guidance = "Nobody answered. Do not ask again: pick the most reasonable option yourself, state the assumption you made, and continue."
		}
		return out, nil
	}).Description("Ask the user one question when their answer changes what you do next. Do not use it for questions you can answer by reading the workspace. If nobody is available the result says so, and you should proceed on your own judgement.").Build()
	if err != nil {
		return nil, err
	}
	return agentkit.Pack(tool), nil
}

func unansweredAsk(reason string) AskUserOutput {
	return AskUserOutput{
		Selected: -1,
		Reason:   reason,
		Guidance: "Nobody answered. Do not ask again: pick the most reasonable option yourself, state the assumption you made, and continue.",
	}
}
