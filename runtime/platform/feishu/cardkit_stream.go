package feishu

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
	larkcardkit "github.com/larksuite/oapi-sdk-go/v3/service/cardkit/v1"
)

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

// patchRichCard closes CardKit streaming (when needed) then replaces the full card JSON.
func (p *Platform) patchRichCard(ctx context.Context, handle any, cardJSON string) error {
	h, ok := handle.(*feishuPreviewHandle)
	if !ok || h == nil || strings.TrimSpace(h.cardID) == "" || !isCardJSON(cardJSON) {
		return p.UpdateMessage(ctx, handle, cardJSON)
	}
	if err := p.closeCardStreaming(ctx, h); err != nil && !isCardStreamingClosedError(err) {
		slog.Warn(p.tag()+": close card streaming before rich patch failed", "card_id", h.cardID, "error", err)
	}
	h.mu.Lock()
	h.streaming = false
	h.elementID = ""
	h.mu.Unlock()
	return p.updateCardEntity(ctx, h, cardJSON)
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

func (p *Platform) streamCardElementByID(ctx context.Context, h *feishuPreviewHandle, elementID, content string) error {
	if strings.TrimSpace(elementID) == "" {
		return fmt.Errorf("%s: stream card content: empty element_id", p.tag())
	}
	seq := h.nextSequence()
	req := larkcardkit.NewContentCardElementReqBuilder().
		CardId(h.cardID).
		ElementId(elementID).
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

const larkErrCardStreamingClosed = 300309

func isCardStreamingClosedError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "streaming mode is closed") ||
		strings.Contains(msg, fmt.Sprintf("code=%d", larkErrCardStreamingClosed))
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
