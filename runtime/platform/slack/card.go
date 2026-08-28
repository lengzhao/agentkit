package slack

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync/atomic"

	"github.com/lengzhao/agentkit/runtime/platform/common"
	"github.com/slack-go/slack"
)

const cardActionIDPrefix = "cc_card_"

var cardActionSeq uint64

type cardMessageRef struct {
	channel  string
	ts       string
	threadTS string
}

func nextCardActionID() string {
	return fmt.Sprintf("%s%d", cardActionIDPrefix, atomic.AddUint64(&cardActionSeq, 1))
}

func encodeActionValue(action, sessionKey string, extra map[string]string) string {
	payload := map[string]string{"action": action}
	if sessionKey != "" {
		payload["session_key"] = sessionKey
	}
	for k, v := range extra {
		if k == "" || v == "" {
			continue
		}
		payload[k] = v
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return action
	}
	return string(b)
}

func decodeActionValue(raw string) (action, sessionKey string, extra map[string]string) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", "", nil
	}
	var payload map[string]string
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return raw, "", nil
	}
	action = payload["action"]
	sessionKey = payload["session_key"]
	extra = make(map[string]string, len(payload))
	for k, v := range payload {
		if k == "action" || k == "session_key" {
			continue
		}
		extra[k] = v
	}
	if len(extra) == 0 {
		extra = nil
	}
	return action, sessionKey, extra
}

func plainTextBlock(text string) *slack.TextBlockObject {
	return slack.NewTextBlockObject(slack.PlainTextType, text, true, false)
}

func mrkdwnBlock(text string) *slack.TextBlockObject {
	return slack.NewTextBlockObject(slack.MarkdownType, text, false, false)
}

func slackButtonStyle(btnType string) slack.Style {
	switch btnType {
	case "primary":
		return slack.StylePrimary
	case "danger":
		return slack.StyleDanger
	default:
		return ""
	}
}

func newButtonElement(text, btnType, action, sessionKey string, extra map[string]string) *slack.ButtonBlockElement {
	btn := slack.NewButtonBlockElement(
		nextCardActionID(),
		encodeActionValue(action, sessionKey, extra),
		plainTextBlock(text),
	)
	if style := slackButtonStyle(btnType); style != "" {
		btn.WithStyle(style)
	}
	return btn
}

func renderCardBlocks(card *common.Card, sessionKey string) []slack.Block {
	if card == nil {
		return []slack.Block{slack.NewSectionBlock(mrkdwnBlock(" "), nil, nil)}
	}
	var blocks []slack.Block
	if card.Header != nil && card.Header.Title != "" {
		blocks = append(blocks, slack.NewHeaderBlock(plainTextBlock(card.Header.Title)))
	}
	for _, elem := range card.Elements {
		switch e := elem.(type) {
		case common.CardMarkdown:
			if strings.TrimSpace(e.Content) == "" {
				continue
			}
			blocks = append(blocks, slack.NewSectionBlock(mrkdwnBlock(e.Content), nil, nil))
		case common.CardListItem:
			btnType := e.BtnType
			if btnType == "" {
				btnType = "default"
			}
			accessory := slack.NewAccessory(newButtonElement(e.BtnText, btnType, e.BtnValue, sessionKey, e.Extra))
			blocks = append(blocks, slack.NewSectionBlock(mrkdwnBlock(e.Text), nil, accessory))
		case common.CardNote:
			if strings.TrimSpace(e.Text) == "" {
				continue
			}
			blocks = append(blocks, slack.NewContextBlock("", plainTextBlock(e.Text)))
		}
	}
	if len(blocks) == 0 {
		blocks = append(blocks, slack.NewSectionBlock(mrkdwnBlock(" "), nil, nil))
	}
	return blocks
}

func (p *Platform) trackCardMessage(sessionKey, channel, ts, threadTS string) {
	if sessionKey == "" || channel == "" || ts == "" {
		return
	}
	p.cardMsgMu.Lock()
	if p.cardMsgRefs == nil {
		p.cardMsgRefs = make(map[string]cardMessageRef)
	}
	p.cardMsgRefs[sessionKey] = cardMessageRef{channel: channel, ts: ts, threadTS: threadTS}
	p.cardMsgMu.Unlock()
}

func (p *Platform) postCard(ctx context.Context, d delivery, card *common.Card) error {
	sessionKey := string(d.sessionID)
	blocks := renderCardBlocks(card, sessionKey)
	opts := []slack.MsgOption{
		slack.MsgOptionBlocks(blocks...),
		slack.MsgOptionText(card.FallbackText(), false),
	}
	if d.replyInThread() {
		opts = append(opts, slack.MsgOptionPostMessageParameters(slack.PostMessageParameters{
			ThreadTimestamp: d.threadTS,
		}))
	}
	_, ts, err := p.client.PostMessageContext(ctx, d.channel, opts...)
	if err != nil {
		return fmt.Errorf("slack: post card: %w", err)
	}
	p.trackCardMessage(sessionKey, d.channel, ts, d.threadTS)
	return nil
}

func (p *Platform) updateCardMessage(ctx context.Context, ref cardMessageRef, card *common.Card, sessionKey string) error {
	blocks := renderCardBlocks(card, sessionKey)
	_, _, _, err := p.client.UpdateMessageContext(
		ctx,
		ref.channel,
		ref.ts,
		slack.MsgOptionBlocks(blocks...),
		slack.MsgOptionText(card.FallbackText(), false),
	)
	if err != nil {
		return fmt.Errorf("slack: update card: %w", err)
	}
	return nil
}
