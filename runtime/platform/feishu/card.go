package feishu

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
	"github.com/lengzhao/agentkit/runtime/cardcompat"
	"github.com/lengzhao/agentkit/runtime/platform/common"
)

// listItemInlineMaxRunes is the longest option text that still fits beside a button.
const listItemInlineMaxRunes = 16

// cardButtonMaxRunes keeps Feishu button labels from overflowing the control.
const cardButtonMaxRunes = 12

func plainText(content string) map[string]any {
	return map[string]any{"tag": "plain_text", "content": content}
}

func (p *Platform) ReplyCard(ctx context.Context, rctx replyContext, card *common.Card) error {
	cardJSON := renderCard(card, rctx.sessionKey)
	if !p.shouldUseThreadOrReplyAPI(rctx) {
		if rctx.chatID == "" {
			return fmt.Errorf("%s: chatID is empty, cannot send card", p.tag())
		}
		return p.createMessage(ctx, rctx.chatID, larkim.MsgTypeInteractive, cardJSON, "send card")
	}
	return p.replyMessage(ctx, rctx, larkim.MsgTypeInteractive, cardJSON)
}

func (p *Platform) RefreshCard(ctx context.Context, sessionKey string, card *common.Card) error {
	p.cardActionMsgMu.Lock()
	msgID := p.cardActionMsgIDs[sessionKey]
	p.cardActionMsgMu.Unlock()
	if msgID == "" {
		return fmt.Errorf("%s: no tracked card messageID for session %q", p.tag(), sessionKey)
	}
	cardJSON := renderCard(card, sessionKey)
	req := larkim.NewPatchMessageReqBuilder().
		MessageId(msgID).
		Body(larkim.NewPatchMessageReqBodyBuilder().Content(cardJSON).Build()).
		Build()
	return p.withTransientRetry(ctx, "refresh card", func() error {
		return p.withFreshTenantAccessTokenRetry(ctx, "refresh card", func(client *lark.Client, options ...larkcore.RequestOptionFunc) error {
			resp, err := client.Im.Message.Patch(ctx, req, options...)
			if err != nil {
				return fmt.Errorf("%s: refresh card: %w", p.tag(), err)
			}
			if !resp.Success() {
				return fmt.Errorf("%s: refresh card code=%d msg=%s", p.tag(), resp.Code, resp.Msg)
			}
			return nil
		})
	})
}

func renderCardMap(card *common.Card, sessionKey string) map[string]any {
	card = cardcompat.Normalize(card)
	config := map[string]any{"wide_screen_mode": true}
	if card != nil && card.Static {
		config["enable_forward_interaction"] = false
		config["update_multi"] = false
	}
	result := map[string]any{
		"config": config,
	}
	if card == nil {
		return result
	}
	if card.Header != nil && card.Header.Title != "" {
		color := card.Header.Color
		if color == "" {
			color = "blue"
		}
		result["header"] = map[string]any{
			"title":    plainText(card.Header.Title),
			"template": color,
		}
	}
	elements := renderCardElements(card.Elements, sessionKey, card.Static)
	if len(elements) == 0 {
		elements = []map[string]any{{"tag": "markdown", "content": " "}}
	}
	result["elements"] = elements
	return result
}

func renderCardElements(source []common.CardElement, sessionKey string, static bool) []map[string]any {
	var elements []map[string]any
	for _, elem := range source {
		switch e := elem.(type) {
		case common.CardForm:
			fields := renderCardElements(e.Elements, sessionKey, static)
			if static {
				elements = append(elements, fields...)
				continue
			}
			var columns []map[string]any
			for _, button := range e.Buttons {
				columns = append(columns, map[string]any{"tag": "column", "width": "auto", "vertical_align": "center", "elements": []map[string]any{renderCardButton(button, sessionKey)}})
			}
			if len(columns) > 0 {
				fields = append(fields, map[string]any{"tag": "column_set", "horizontal_align": "left", "columns": columns})
			}
			elements = append(elements, map[string]any{"tag": "form", "name": e.Name, "elements": fields})
		case common.CardCheckbox:
			if static {
				mark := "◻ "
				if e.Checked {
					mark = "☑ "
				}
				elements = append(elements, map[string]any{"tag": "markdown", "content": mark + e.Text})
				continue
			}
			elements = append(elements, map[string]any{"tag": "checker", "name": e.Name, "checked": e.Checked, "text": map[string]any{"tag": "lark_md", "content": e.Text}})
		case common.CardMarkdown:
			elements = append(elements, map[string]any{"tag": "markdown", "content": e.Content})
		case common.CardDivider:
			elements = append(elements, map[string]any{"tag": "hr"})
		case common.CardActions:
			if static {
				continue
			}
			var actions []map[string]any
			for _, btn := range e.Buttons {
				action := renderCardButton(btn, sessionKey)
				if e.Layout == common.CardActionLayoutEqualColumns {
					action["width"] = "fill"
				}
				actions = append(actions, action)
			}
			if len(actions) > 0 {
				if e.Layout == common.CardActionLayoutEqualColumns {
					columns := make([]map[string]any, 0, len(actions))
					for _, action := range actions {
						columns = append(columns, map[string]any{
							"tag": "column", "width": "weighted", "weight": 1,
							"vertical_align": "center", "horizontal_align": "center",
							"elements": []map[string]any{action},
						})
					}
					columnSet := map[string]any{"tag": "column_set", "columns": columns}
					if len(actions) == 2 {
						columnSet["flex_mode"] = "bisect"
					}
					elements = append(elements, columnSet)
				} else {
					elements = append(elements, map[string]any{"tag": "action", "actions": actions})
				}
			}
		case common.CardListItem:
			if static {
				if strings.TrimSpace(e.Text) == "" {
					continue
				}
				elements = append(elements, map[string]any{"tag": "markdown", "content": e.Text})
				continue
			}
			btnType := e.BtnType
			if btnType == "" {
				btnType = "default"
			}
			valMap := map[string]string{"action": e.BtnValue}
			if sessionKey != "" {
				valMap["session_key"] = sessionKey
			}
			for k, v := range e.Extra {
				valMap[k] = v
			}
			btn := map[string]any{
				"tag": "button", "text": plainText(truncateRunes(e.BtnText, cardButtonMaxRunes)),
				"type": btnType, "value": valMap,
			}
			if listItemStacksVertically(e.Text) {
				btn["width"] = "fill"
				elements = append(elements, map[string]any{
					"tag": "column_set", "flex_mode": "none",
					"columns": []map[string]any{
						{"tag": "column", "width": "weighted", "weight": 1, "vertical_align": "top",
							"elements": []map[string]any{
								{"tag": "markdown", "content": e.Text},
								btn,
							}},
					},
				})
				continue
			}
			elements = append(elements, map[string]any{
				"tag": "column_set", "flex_mode": "none",
				"columns": []map[string]any{
					{"tag": "column", "width": "weighted", "weight": 5, "vertical_align": "center",
						"elements": []map[string]any{{"tag": "markdown", "content": e.Text}}},
					{"tag": "column", "width": "auto", "vertical_align": "center",
						"elements": []map[string]any{btn}},
				},
			})
		case common.CardSelect:
			if static {
				continue
			}
			var options []map[string]any
			for _, opt := range e.Options {
				options = append(options, map[string]any{"text": plainText(opt.Text), "value": opt.Value})
			}
			selectElem := map[string]any{
				"tag": "select_static", "placeholder": plainText(e.Placeholder), "options": options,
			}
			if sessionKey != "" {
				selectElem["value"] = map[string]string{"session_key": sessionKey}
			}
			if e.InitValue != "" {
				selectElem["initial_option"] = e.InitValue
			}
			elements = append(elements, map[string]any{"tag": "action", "actions": []map[string]any{selectElem}})
		case common.CardNote:
			if strings.TrimSpace(e.Text) == "" {
				continue
			}
			elements = append(elements, map[string]any{"tag": "note", "elements": []map[string]any{plainText(e.Text)}})
		}
	}
	return elements
}

func listItemStacksVertically(text string) bool {
	text = strings.TrimSpace(text)
	if text == "" {
		return false
	}
	if strings.Contains(text, "\n") {
		return true
	}
	return len([]rune(text)) > listItemInlineMaxRunes
}

func renderCard(card *common.Card, sessionKey string) string {
	b, err := json.Marshal(renderCardMap(card, sessionKey))
	if err != nil {
		slog.Error("feishu: renderCard marshal failed", "error", err)
		return `{"config":{"wide_screen_mode":true},"elements":[]}`
	}
	return string(b)
}

func renderCardButton(button common.CardButton, sessionKey string) map[string]any {
	kind := button.Type
	if kind == "" {
		kind = "default"
	}
	value := map[string]string{"action": button.Value}
	if sessionKey != "" {
		value["session_key"] = sessionKey
	}
	for key, item := range button.Extra {
		value[key] = item
	}
	out := map[string]any{"tag": "button", "text": plainText(button.Text), "type": kind, "value": value}
	if button.Name != "" {
		out["name"] = button.Name
	}
	if button.FormAction != "" {
		out["form_action_type"] = button.FormAction
	}
	return out
}
