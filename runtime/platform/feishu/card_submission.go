package feishu

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
	"github.com/lengzhao/agentkit/cap/interaction"
	"github.com/lengzhao/agentkit/runtime/platform/common"
)

func normalizeFeishuInteraction(e *interaction.Event) {
	var a callback.CallBackAction
	_ = json.Unmarshal(e.RawAction, &a)
	e.Purpose = interaction.PurposeSubmit
	e.SubmissionMode = interaction.SubmissionOnce
	inForm, formAction := common.InteractionControlForm(e.RawCard, a.Name)
	if strings.HasPrefix(e.Action, "nav:") || strings.HasPrefix(e.Action, "cmd:") || strings.HasPrefix(e.Action, "act:") {
		e.Purpose = interaction.PurposeNavigate
		e.SubmissionMode = interaction.SubmissionRepeat
	} else if !json.Valid(e.RawCard) {
		// Without the original layout, retain the answer without locking a possible draft/reset.
		e.SubmissionMode = interaction.SubmissionRepeat
	} else if inForm && (a.Tag != "button" || formAction != "submit") {
		e.Purpose = interaction.PurposeChange
	}
	common.ApplyInteractionPolicy(e, a.Value["agentkit_interaction"])
}
func (p *Platform) closeInteractionCard(ctx context.Context, e interaction.Event, text string) error {
	if p.client == nil || e.MessageID == "" {
		return fmt.Errorf("card update client or message ID unavailable")
	}
	content, err := submittedFeishuCard(e.RawCard, text)
	if err != nil {
		return err
	}
	req := larkim.NewPatchMessageReqBuilder().MessageId(e.MessageID).Body(larkim.NewPatchMessageReqBodyBuilder().Content(content).Build()).Build()
	return p.withTransientRetry(ctx, "close submitted card", func() error {
		return p.withFreshTenantAccessTokenRetry(ctx, "close submitted card", func(client *lark.Client, opts ...larkcore.RequestOptionFunc) error {
			resp, err := client.Im.Message.Patch(ctx, req, opts...)
			if err != nil {
				return err
			}
			if resp == nil || !resp.Success() {
				return fmt.Errorf("close submitted card failed: %v", resp)
			}
			return nil
		})
	})
}
func (p *Platform) notifyInteractionUser(ctx context.Context, e interaction.Event, text string) error {
	if p.client == nil || e.UserID == "" {
		return fmt.Errorf("personal interaction notice unavailable")
	}
	// A shared question remains interactive for other users. Do not replace it.
	_, body := buildPlainTextContent(text)
	return p.createMessage(ctx, e.UserID, larkim.MsgTypeText, body, "send interaction receipt")
}
