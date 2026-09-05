package feishu

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
	larkcardkit "github.com/larksuite/oapi-sdk-go/v3/service/cardkit/v1"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

const (
	bodyStreamElementID   = "body_md"
	streamCardKitInterval = 100 * time.Millisecond
)

func (p *Platform) useCardKitStream() bool {
	return p.useInteractiveCard
}

func streamingCardConfig() map[string]any {
	return map[string]any{
		"streaming_mode": true,
		"update_multi":   true,
		"streaming_config": map[string]any{
			"print_frequency_ms": map[string]any{
				"default": 70,
				"android": 70,
				"ios":     70,
				"pc":      70,
			},
			"print_step": map[string]any{
				"default": 2,
				"android": 2,
				"ios":     2,
				"pc":      2,
			},
			"print_strategy": "fast",
		},
	}
}

func buildStreamingBodyCardEntityJSON() string {
	card := map[string]any{
		"schema": "2.0",
		"config": streamingCardConfig(),
		"body": map[string]any{
			"elements": []map[string]any{
				{
					"tag":        "markdown",
					"element_id": bodyStreamElementID,
					"content":    "",
				},
			},
		},
	}
	b, _ := json.Marshal(card)
	return string(b)
}

func buildIMCardEntityContent(cardID string) string {
	payload := map[string]any{
		"type": "card",
		"data": map[string]any{
			"card_id": cardID,
		},
	}
	b, _ := json.Marshal(payload)
	return string(b)
}

func (h *feishuPreviewHandle) nextSequence() int {
	h.mu.Lock()
	h.sequence++
	seq := h.sequence
	h.mu.Unlock()
	return seq
}

func (p *Platform) createCardEntity(ctx context.Context, cardJSON string) (string, error) {
	req := larkcardkit.NewCreateCardReqBuilder().
		Body(larkcardkit.NewCreateCardReqBodyBuilder().
			Type("card_json").
			Data(cardJSON).
			Build()).
		Build()

	var resp *larkcardkit.CreateCardResp
	err := p.withTransientRetry(ctx, "create card entity", func() error {
		return p.withFreshTenantAccessTokenRetry(ctx, "create card entity", func(client *lark.Client, options ...larkcore.RequestOptionFunc) error {
			var callErr error
			resp, callErr = client.Cardkit.V1.Card.Create(ctx, req, options...)
			if callErr != nil {
				return fmt.Errorf("%s: create card entity: %w", p.tag(), callErr)
			}
			if !resp.Success() {
				return fmt.Errorf("%s: create card entity code=%d msg=%s", p.tag(), resp.Code, resp.Msg)
			}
			return nil
		})
	})
	if err != nil {
		return "", err
	}
	if resp.Data == nil || resp.Data.CardId == nil || *resp.Data.CardId == "" {
		return "", fmt.Errorf("%s: create card entity: empty card_id", p.tag())
	}
	return *resp.Data.CardId, nil
}

func (p *Platform) sendCardEntityIM(ctx context.Context, rc replyContext, cardID string) (string, error) {
	content := buildIMCardEntityContent(cardID)
	var msgID string

	if p.shouldUseThreadOrReplyAPI(rc) {
		req := larkim.NewReplyMessageReqBuilder().
			MessageId(rc.messageID).
			Body(p.buildReplyMessageReqBody(rc, larkim.MsgTypeInteractive, content)).
			Build()
		var resp *larkim.ReplyMessageResp
		if err := p.withTransientRetry(ctx, "send card entity", func() error {
			return p.withFreshTenantAccessTokenRetry(ctx, "send card entity", func(client *lark.Client, options ...larkcore.RequestOptionFunc) error {
				var callErr error
				resp, callErr = client.Im.Message.Reply(ctx, req, options...)
				if callErr != nil {
					return fmt.Errorf("%s: send card entity (reply): %w", p.tag(), callErr)
				}
				if !resp.Success() {
					return fmt.Errorf("%s: send card entity (reply) code=%d msg=%s", p.tag(), resp.Code, resp.Msg)
				}
				return nil
			})
		}); err != nil {
			return "", err
		}
		if resp.Data != nil && resp.Data.MessageId != nil {
			msgID = *resp.Data.MessageId
		}
	} else {
		req := larkim.NewCreateMessageReqBuilder().
			ReceiveIdType("chat_id").
			Body(larkim.NewCreateMessageReqBodyBuilder().
				ReceiveId(rc.chatID).
				MsgType(larkim.MsgTypeInteractive).
				Content(content).
				Build()).
			Build()
		var resp *larkim.CreateMessageResp
		if err := p.withTransientRetry(ctx, "send card entity", func() error {
			return p.withFreshTenantAccessTokenRetry(ctx, "send card entity", func(client *lark.Client, options ...larkcore.RequestOptionFunc) error {
				var callErr error
				resp, callErr = client.Im.Message.Create(ctx, req, options...)
				if callErr != nil {
					return fmt.Errorf("%s: send card entity: %w", p.tag(), callErr)
				}
				if !resp.Success() {
					return fmt.Errorf("%s: send card entity code=%d msg=%s", p.tag(), resp.Code, resp.Msg)
				}
				return nil
			})
		}); err != nil {
			return "", err
		}
		if resp.Data != nil && resp.Data.MessageId != nil {
			msgID = *resp.Data.MessageId
		}
	}

	if msgID == "" {
		return "", fmt.Errorf("%s: send card entity: no message ID returned", p.tag())
	}
	return msgID, nil
}

func (p *Platform) createAndSendCardEntity(ctx context.Context, rc replyContext, cardJSON string, streamElementID string) (*feishuPreviewHandle, error) {
	cardID, err := p.createCardEntity(ctx, cardJSON)
	if err != nil {
		return nil, err
	}
	msgID, err := p.sendCardEntityIM(ctx, rc, cardID)
	if err != nil {
		return nil, err
	}
	return &feishuPreviewHandle{
		messageID: msgID,
		chatID:    rc.chatID,
		cardID:    cardID,
		elementID: streamElementID,
		streaming: streamElementID != "",
	}, nil
}

func (p *Platform) updateCardEntity(ctx context.Context, h *feishuPreviewHandle, cardJSON string) error {
	seq := h.nextSequence()
	card := larkcardkit.NewCardBuilder().
		Type("card_json").
		Data(cardJSON).
		Build()
	req := larkcardkit.NewUpdateCardReqBuilder().
		CardId(h.cardID).
		Body(larkcardkit.NewUpdateCardReqBodyBuilder().
			Card(card).
			Sequence(seq).
			Build()).
		Build()

	return p.withTransientRetry(ctx, "update card entity", func() error {
		return p.withFreshTenantAccessTokenRetry(ctx, "update card entity", func(client *lark.Client, options ...larkcore.RequestOptionFunc) error {
			resp, err := client.Cardkit.V1.Card.Update(ctx, req, options...)
			if err != nil {
				return fmt.Errorf("%s: update card entity: %w", p.tag(), err)
			}
			if !resp.Success() {
				return fmt.Errorf("%s: update card entity code=%d msg=%s", p.tag(), resp.Code, resp.Msg)
			}
			return nil
		})
	})
}

func (p *Platform) streamCardElementContent(ctx context.Context, h *feishuPreviewHandle, content string) error {
	seq := h.nextSequence()
	req := larkcardkit.NewContentCardElementReqBuilder().
		CardId(h.cardID).
		ElementId(h.elementID).
		Body(larkcardkit.NewContentCardElementReqBodyBuilder().
			Content(content).
			Sequence(seq).
			Build()).
		Build()

	return p.withTransientRetry(ctx, "stream card content", func() error {
		return p.withFreshTenantAccessTokenRetry(ctx, "stream card content", func(client *lark.Client, options ...larkcore.RequestOptionFunc) error {
			resp, err := client.Cardkit.V1.CardElement.Content(ctx, req, options...)
			if err != nil {
				return fmt.Errorf("%s: stream card content: %w", p.tag(), err)
			}
			if !resp.Success() {
				return fmt.Errorf("%s: stream card content code=%d msg=%s", p.tag(), resp.Code, resp.Msg)
			}
			return nil
		})
	})
}

func (p *Platform) closeCardStreaming(ctx context.Context, h *feishuPreviewHandle) error {
	seq := h.nextSequence()
	settings := map[string]any{
		"config": map[string]any{
			"streaming_mode": false,
		},
	}
	settingsJSON, err := json.Marshal(settings)
	if err != nil {
		return err
	}
	req := larkcardkit.NewSettingsCardReqBuilder().
		CardId(h.cardID).
		Body(larkcardkit.NewSettingsCardReqBodyBuilder().
			Settings(string(settingsJSON)).
			Sequence(seq).
			Build()).
		Build()

	return p.withTransientRetry(ctx, "close card streaming", func() error {
		return p.withFreshTenantAccessTokenRetry(ctx, "close card streaming", func(client *lark.Client, options ...larkcore.RequestOptionFunc) error {
			resp, err := client.Cardkit.V1.Card.Settings(ctx, req, options...)
			if err != nil {
				return fmt.Errorf("%s: close card streaming: %w", p.tag(), err)
			}
			if !resp.Success() {
				return fmt.Errorf("%s: close card streaming code=%d msg=%s", p.tag(), resp.Code, resp.Msg)
			}
			return nil
		})
	})
}

func (p *Platform) finalizeStreamingBodyCard(ctx context.Context, h *feishuPreviewHandle, text string) error {
	processed := text
	if containsMarkdown(text) {
		processed = preprocessFeishuMarkdown(text)
	}
	processed = sanitizeMarkdownURLs(processed)

	if h.elementID != "" {
		if err := p.streamCardElementContent(ctx, h, processed); err != nil {
			return err
		}
	}
	if err := p.closeCardStreaming(ctx, h); err != nil {
		return err
	}
	h.mu.Lock()
	h.elementID = ""
	h.streaming = false
	h.mu.Unlock()
	return p.updateCardEntity(ctx, h, buildFinalPreviewCardJSON(text))
}
