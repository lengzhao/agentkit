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

// larkErrCardSeqConflict 是 CardKit 300317「sequence number compare failed」：
// 本帧已被更新的状态取代。卡片内容单调累积（每次 flush 渲染全量快照），被拒的
// 旧帧直接丢弃即为正确语义——重试同 seq 必然再撞 300317，上抛则会误触卡片重建。
const larkErrCardSeqConflict = 300317

// isSeqConflictResponse 识别 300317 并记 debug 日志；返回 true 表示调用方应按
// 成功处理（新帧已在服务端生效，或丢失响应的请求实际已落地）。
func (p *Platform) isSeqConflictResponse(operation, cardID string, code int, msg string) bool {
	if code != larkErrCardSeqConflict {
		return false
	}
	slog.Debug(p.tag()+": "+operation+" superseded by newer frame (seq conflict), dropping stale frame",
		"card_id", cardID, "code", code, "msg", msg)
	return true
}

// patchRichCard closes CardKit streaming (when needed) then replaces the full card JSON.
// profile 决定 transient 重试预算：中途帧传 retryProfileQuick/Standard，
// turn/end 定稿传 retryProfileFinalize 以熬过出口网关故障窗口。
func (p *Platform) patchRichCard(ctx context.Context, handle any, cardJSON string, profile transientRetryProfile) error {
	h, ok := handle.(*feishuPreviewHandle)
	if !ok || h == nil || strings.TrimSpace(h.cardID) == "" || !isCardJSON(cardJSON) {
		return p.updateMessageProfile(ctx, handle, cardJSON, profile)
	}
	// close + update 必须在同一次持锁内完成，避免两个原子操作之间被其他写入插入。
	h.reqMu.Lock()
	defer h.reqMu.Unlock()
	if err := p.closeCardStreamingLocked(ctx, h, profile); err != nil && !isCardStreamingClosedError(err) {
		slog.Warn(p.tag()+": close card streaming before rich patch failed", "card_id", h.cardID, "error", err)
	}
	h.mu.Lock()
	h.streaming = false
	h.elementID = ""
	h.mu.Unlock()
	return p.updateCardEntityLocked(ctx, h, cardJSON, profile)
}

func (p *Platform) updateCardEntity(ctx context.Context, h *feishuPreviewHandle, cardJSON string, profile transientRetryProfile) error {
	h.reqMu.Lock()
	defer h.reqMu.Unlock()
	return p.updateCardEntityLocked(ctx, h, cardJSON, profile)
}

// updateCardEntityLocked 调用方须持有 h.reqMu：取 seq 与 HTTP 往返整体串行，
// 保证同一 card_id 的请求按取号顺序到达服务端。
func (p *Platform) updateCardEntityLocked(ctx context.Context, h *feishuPreviewHandle, cardJSON string, profile transientRetryProfile) error {
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

	return p.withTransientRetryProfile(ctx, "update card entity", profile, func() error {
		return p.withFreshTenantAccessTokenRetry(ctx, "update card entity", func(client *lark.Client, options ...larkcore.RequestOptionFunc) error {
			resp, err := client.Cardkit.V1.Card.Update(ctx, req, options...)
			if err != nil {
				return fmt.Errorf("%s: update card entity: %w", p.tag(), err)
			}
			if p.isSeqConflictResponse("update card entity", h.cardID, resp.Code, resp.Msg) {
				return nil
			}
			if !resp.Success() {
				return fmt.Errorf("%s: update card entity code=%d msg=%s", p.tag(), resp.Code, resp.Msg)
			}
			return nil
		})
	})
}

func (p *Platform) streamCardElementByID(ctx context.Context, h *feishuPreviewHandle, elementID, content string, profile transientRetryProfile) error {
	if strings.TrimSpace(elementID) == "" {
		return fmt.Errorf("%s: stream card content: empty element_id", p.tag())
	}
	h.reqMu.Lock()
	defer h.reqMu.Unlock()
	seq := h.nextSequence()
	req := larkcardkit.NewContentCardElementReqBuilder().
		CardId(h.cardID).
		ElementId(elementID).
		Body(larkcardkit.NewContentCardElementReqBodyBuilder().
			Content(content).
			Sequence(seq).
			Build()).
		Build()

	return p.withTransientRetryProfile(ctx, "stream card content", profile, func() error {
		return p.withFreshTenantAccessTokenRetry(ctx, "stream card content", func(client *lark.Client, options ...larkcore.RequestOptionFunc) error {
			resp, err := client.Cardkit.V1.CardElement.Content(ctx, req, options...)
			if err != nil {
				return fmt.Errorf("%s: stream card content: %w", p.tag(), err)
			}
			if p.isSeqConflictResponse("stream card content", h.cardID, resp.Code, resp.Msg) {
				return nil
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

// closeCardStreamingLocked 调用方须持有 h.reqMu（见 patchRichCard）。
func (p *Platform) closeCardStreamingLocked(ctx context.Context, h *feishuPreviewHandle, profile transientRetryProfile) error {
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

	return p.withTransientRetryProfile(ctx, "close card streaming", profile, func() error {
		return p.withFreshTenantAccessTokenRetry(ctx, "close card streaming", func(client *lark.Client, options ...larkcore.RequestOptionFunc) error {
			resp, err := client.Cardkit.V1.Card.Settings(ctx, req, options...)
			if err != nil {
				return fmt.Errorf("%s: close card streaming: %w", p.tag(), err)
			}
			if p.isSeqConflictResponse("close card streaming", h.cardID, resp.Code, resp.Msg) {
				return nil
			}
			if !resp.Success() {
				return fmt.Errorf("%s: close card streaming code=%d msg=%s", p.tag(), resp.Code, resp.Msg)
			}
			return nil
		})
	})
}
