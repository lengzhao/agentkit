package askuser

import (
	"context"
	"fmt"
	"strings"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/permission"
	rtpermission "github.com/lengzhao/agentkit/runtime/permission"
)

type AskUserConfig struct{}

type AskUserDeps struct{}

type AskUserInput struct {
	Question string   `json:"question" jsonschema:"One specific question whose answer changes what you do next"`
	Options  []string `json:"options,omitempty" jsonschema:"Offer concrete choices when the answer is one of a few known options"`
	Default  string   `json:"default,omitempty" jsonschema:"Answer to assume when the user just presses enter"`
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
//   - Routes through the inbound platform via permission.Broker; interactive CLI renders permission/request, IM platforms render cards/buttons.
//   - Headless platforms return answered=false immediately; it is never an error and never blocks the turn.
//   - Do not mount it on subagents: a child agent runs behind a delegate call, where nobody is watching its stdout.
func NewAskUser(_ AskUserConfig, _ AskUserDeps) (agentkit.Tool, error) {
	tool, err := agentkit.NewTool("ask_user", func(ctx context.Context, input AskUserInput) (AskUserOutput, error) {
		question := strings.TrimSpace(input.Question)
		if question == "" {
			return AskUserOutput{}, fmt.Errorf("question is required")
		}

		broker, ok := rtpermission.BrokerFrom(ctx)
		if !ok {
			return unansweredAsk("no permission broker on this session"), nil
		}

		options := make([]permission.Option, 0, len(input.Options))
		for _, opt := range input.Options {
			options = append(options, permission.Option{Label: opt})
		}

		result, err := broker.Await(ctx, permission.Request{
			Kind: permission.KindQuestion,
			Question: &permission.Question{
				Prompt:  question,
				Options: options,
				Default: strings.TrimSpace(input.Default),
			},
		})
		if err != nil {
			return AskUserOutput{}, err
		}
		return mapAskUserOutput(result), nil
	}).Description("Ask the user one question when their answer changes what you do next. Do not use it for questions you can answer by reading the workspace. If nobody is available the result says so, and you should proceed on your own judgement.").Build()
	if err != nil {
		return nil, err
	}
	return tool, nil
}

func mapAskUserOutput(result permission.Result) AskUserOutput {
	out := AskUserOutput{
		Answered: result.Resolved(),
		Reason:   askUserReason(result),
		Guidance: result.Guidance,
		Selected: -1,
	}
	if result.Answer != nil {
		out.Answer = result.Answer.Text
		if len(result.Answer.Selected) > 0 {
			out.Selected = result.Answer.Selected[0]
		}
	}
	if !result.Resolved() && out.Guidance == "" {
		out.Guidance = "Nobody answered. Do not ask again: pick the most reasonable option yourself, state the assumption you made, and continue."
	}
	return out
}

func askUserReason(result permission.Result) string {
	if result.Reason != "" {
		if !result.Resolved() {
			return string(result.Outcome) + ": " + result.Reason
		}
		return result.Reason
	}
	if !result.Resolved() {
		return string(result.Outcome)
	}
	return ""
}

func unansweredAsk(reason string) AskUserOutput {
	return AskUserOutput{
		Selected: -1,
		Reason:   reason,
		Guidance: "Nobody answered. Do not ask again: pick the most reasonable option yourself, state the assumption you made, and continue.",
	}
}
