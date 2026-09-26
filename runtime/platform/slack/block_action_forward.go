package slack

import (
	"context"
	"log/slog"
	"strings"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/platform/common"
	slackapi "github.com/slack-go/slack"
)

func formatSlackBlockActionOperation(action *slackapi.BlockAction, actionVal, rawValue string) string {
	if action == nil {
		if actionVal != "" {
			return "action=" + actionVal
		}
		if rawValue != "" {
			return "value=" + rawValue
		}
		return ""
	}
	var lines []string
	if id := strings.TrimSpace(action.ActionID); id != "" {
		lines = append(lines, "action_id="+id)
	}
	if t := strings.TrimSpace(string(action.Type)); t != "" {
		lines = append(lines, "类型: "+t)
	}
	if actionVal != "" {
		lines = append(lines, "action="+actionVal)
	} else if rawValue != "" && rawValue != actionVal {
		lines = append(lines, "value="+rawValue)
	}
	if v := strings.TrimSpace(action.SelectedOption.Value); v != "" {
		lines = append(lines, "selected="+v)
	}
	if action.SelectedOption.Text != nil {
		if t := strings.TrimSpace(action.SelectedOption.Text.Text); t != "" {
			lines = append(lines, "selected_text="+t)
		}
	}
	if len(action.SelectedOptions) > 0 {
		var opts []string
		for _, o := range action.SelectedOptions {
			if v := strings.TrimSpace(o.Value); v != "" {
				opts = append(opts, v)
			}
		}
		if len(opts) > 0 {
			lines = append(lines, "selected_options="+strings.Join(opts, ", "))
		}
	}
	return strings.Join(lines, "\n")
}

func (p *Platform) fetchBlockMessageText(ctx context.Context, channelID, messageTS string) (string, bool) {
	channelID = strings.TrimSpace(channelID)
	messageTS = strings.TrimSpace(messageTS)
	if channelID == "" || messageTS == "" || p.client == nil {
		return "", false
	}
	resp, err := p.client.GetConversationHistoryContext(ctx, &slackapi.GetConversationHistoryParameters{
		ChannelID: channelID,
		Latest:    messageTS,
		Inclusive: true,
		Limit:     1,
	})
	if err != nil {
		slog.Debug("slack: fetch block message for action failed", "channel", channelID, "ts", messageTS, "error", err)
		return "", false
	}
	if resp == nil || len(resp.Messages) == 0 {
		return "", true
	}
	var msg *slackapi.Message
	for i := range resp.Messages {
		if resp.Messages[i].Timestamp == messageTS {
			msg = &resp.Messages[i]
			break
		}
	}
	if msg == nil {
		msg = &resp.Messages[0]
	}
	return extractSlackBlockMessageText(*msg), true
}

func extractSlackBlockMessageText(msg slackapi.Message) string {
	text := strings.TrimSpace(msg.Text)
	if text != "" {
		return text
	}
	if msg.Blocks.BlockSet == nil {
		return historyFallbackText(msg)
	}
	var parts []string
	for _, block := range msg.Blocks.BlockSet {
		switch b := block.(type) {
		case *slackapi.SectionBlock:
			if b.Text != nil && strings.TrimSpace(b.Text.Text) != "" {
				parts = append(parts, b.Text.Text)
			}
			for _, f := range b.Fields {
				if f != nil && strings.TrimSpace(f.Text) != "" {
					parts = append(parts, f.Text)
				}
			}
		case *slackapi.HeaderBlock:
			if b.Text != nil && strings.TrimSpace(b.Text.Text) != "" {
				parts = append(parts, b.Text.Text)
			}
		case *slackapi.ContextBlock:
			for _, el := range b.ContextElements.Elements {
				if te, ok := el.(*slackapi.TextBlockObject); ok && strings.TrimSpace(te.Text) != "" {
					parts = append(parts, te.Text)
				}
			}
		case *slackapi.RichTextBlock:
			parts = append(parts, extractRichTextBlockPlain(b))
		}
	}
	if len(parts) == 0 {
		return historyFallbackText(msg)
	}
	return strings.Join(parts, "\n")
}

func extractRichTextBlockPlain(b *slackapi.RichTextBlock) string {
	if b == nil {
		return ""
	}
	var parts []string
	for _, el := range b.Elements {
		switch section := el.(type) {
		case *slackapi.RichTextSection:
			for _, sub := range section.Elements {
				if te, ok := sub.(*slackapi.RichTextSectionTextElement); ok {
					if t := strings.TrimSpace(te.Text); t != "" {
						parts = append(parts, t)
					}
				}
			}
		}
	}
	return strings.Join(parts, " ")
}

func blockActionDedupKey(channelID, messageTS, actionID, actionVal string, rawValue string) string {
	return common.InteractionDedupKey(channelID, messageTS, actionID, actionVal, rawValue)
}

func (p *Platform) dispatchCardActionInbound(ctx context.Context, callback slackapi.InteractionCallback, sessionKey, channelID, messageTS, threadTS, content string) {
	channelType := inferSlackChannelType(channelID)
	if !p.channelAllowed(channelID, channelType) {
		return
	}
	direct := isDirectMessageChannel(channelID, channelType)
	sessionID := agentkit.SessionID(sessionKey)
	d := delivery{
		channel:       channelID,
		threadTS:      replyThreadTS(direct, threadTS, messageTS),
		msgTS:         messageTS,
		directMessage: direct,
		sessionID:     sessionID,
	}
	p.deliveries.Store(sessionID, d)
	p.enqueueInbound(ctx, d, callback.User.ID, content, nil, nil, nil, false)
}

func (p *Platform) forwardUnknownBlockAction(ctx context.Context, callback slackapi.InteractionCallback, action *slackapi.BlockAction, sessionKey, channelID, messageTS, threadTS, actionVal, rawValue string) {
	if p.unknownCardAction != common.UnknownInteractionForward {
		return
	}
	if !common.AllowList(p.cfg.AllowFrom, callback.User.ID) {
		return
	}
	channelType := inferSlackChannelType(channelID)
	if !p.channelAllowed(channelID, channelType) {
		return
	}
	actionID := ""
	if action != nil {
		actionID = action.ActionID
	}
	dedupKey := "card_action:" + blockActionDedupKey(channelID, messageTS, actionID, actionVal, rawValue)
	if p.dedup.IsDuplicate(dedupKey) {
		slog.Debug("slack: duplicate block action ignored", "channel", channelID, "ts", messageTS, "action", actionVal)
		return
	}

	cardText, fetched := "", false
	if channelID != "" && messageTS != "" {
		cardText, fetched = p.fetchBlockMessageText(ctx, channelID, messageTS)
	}
	content := common.FormatCardActionInbound(cardText, fetched, formatSlackBlockActionOperation(action, actionVal, rawValue), callback.User.ID)

	slog.Info("slack: unknown block action forwarded to agent",
		"session_key", sessionKey, "user", callback.User.ID, "action", actionVal, "fetched_message", fetched)

	p.dispatchCardActionInbound(ctx, callback, sessionKey, channelID, messageTS, threadTS, content)
}
