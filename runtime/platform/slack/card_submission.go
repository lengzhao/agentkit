package slack

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/lengzhao/agentkit/cap/interaction"
	"github.com/lengzhao/agentkit/runtime/platform/common"
	slackapi "github.com/slack-go/slack"
)

func normalizeSlackInteraction(e *interaction.Event) {
	var raw struct {
		Action *slackapi.BlockAction `json:"action"`
	}
	_ = json.Unmarshal(e.RawAction, &raw)
	e.Purpose = interaction.PurposeSubmit
	e.SubmissionMode = interaction.SubmissionOnce
	if strings.HasPrefix(e.Action, "nav:") || strings.HasPrefix(e.Action, "cmd:") || strings.HasPrefix(e.Action, "act:") {
		e.Purpose = interaction.PurposeNavigate
		e.SubmissionMode = interaction.SubmissionRepeat
	}
	if raw.Action != nil && raw.Action.Type != "button" && !json.Valid(e.RawCard) {
		e.SubmissionMode = interaction.SubmissionRepeat
	}
	if e.Type == string(slackapi.InteractionTypeBlockActions) && (e.InteractionID != "" || (raw.Action != nil && raw.Action.Type != "button" && common.InteractionControlInForm(e.RawCard, raw.Action.ActionID))) {
		e.Purpose = interaction.PurposeChange
	}
	var value map[string]any
	if raw.Action != nil {
		_ = json.Unmarshal([]byte(slackActionValue(*raw.Action)), &value)
	} else {
		var view struct {
			PrivateMetadata string `json:"private_metadata"`
		}
		_ = json.Unmarshal(e.RawCard, &view)
		_ = json.Unmarshal([]byte(view.PrivateMetadata), &value)
	}
	common.ApplyInteractionPolicy(e, value["agentkit_interaction"])
}
func (p *Platform) closeInteractionCard(ctx context.Context, e interaction.Event, text string) error {
	if p.client == nil {
		return fmt.Errorf("card update client unavailable")
	}
	// Native modal submission has already closed the modal when acknowledged.
	if e.Type == string(slackapi.InteractionTypeViewSubmission) {
		return p.notifyInteractionUser(ctx, e, text)
	}
	blocks, err := submittedSlackBlocks(e, text)
	if err != nil {
		return err
	}
	if e.InteractionID != "" {
		var view slackapi.ModalViewRequest
		if err := json.Unmarshal(e.RawCard, &view); err != nil {
			return fmt.Errorf("original modal unavailable: %w", err)
		}
		if view.Title == nil || strings.TrimSpace(view.Title.Text) == "" {
			return fmt.Errorf("original modal title unavailable; preserving modal")
		}
		view.Type = slackapi.VTModal
		view.Blocks = slackapi.Blocks{BlockSet: blocks}
		view.Submit = nil
		if view.Close == nil {
			view.Close = plainTextBlock("关闭")
		}
		_, err := p.client.UpdateViewContext(ctx, view, "", "", e.InteractionID)
		return err
	}
	if e.MessageID == "" {
		return fmt.Errorf("card message ID unavailable")
	}
	_, _, _, err = p.client.UpdateMessageContext(ctx, e.ChatID, e.MessageID, slackapi.MsgOptionBlocks(blocks...), slackapi.MsgOptionText(text, false))
	return err
}
func (p *Platform) notifyInteractionUser(ctx context.Context, e interaction.Event, text string) error {
	if p.client == nil || e.UserID == "" {
		return fmt.Errorf("personal interaction notice unavailable")
	}
	if e.ChatID == e.UserID {
		_, _, err := p.client.PostMessageContext(ctx, e.UserID, slackapi.MsgOptionText(text, false))
		return err
	}
	_, err := p.client.PostEphemeralContext(ctx, e.ChatID, e.UserID, slackapi.MsgOptionText(text, false))
	return err
}

// slackActionValue follows the same value precedence for routing and policy.
func slackActionValue(action slackapi.BlockAction) string {
	if value := strings.TrimSpace(action.Value); value != "" {
		return value
	}
	return strings.TrimSpace(action.SelectedOption.Value)
}
