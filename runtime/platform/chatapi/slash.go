package chatapi

import (
	"context"
	"fmt"
	"strings"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/platform/common"
)

type chatSlashResult struct {
	outcome common.SlashOutcome
}

func (p *Platform) processChatSlash(ctx context.Context, channelKey string, conv *conversation, engineSessionID agentkit.SessionID, query string) (chatSlashResult, error) {
	name, args, ok := common.ParseSlashCommand(query)
	if !ok {
		out, err := common.ProcessSlash(ctx, p.commands, engineSessionID, query)
		return chatSlashResult{outcome: out}, err
	}

	switch name {
	case "new":
		out, err := common.ProcessSlash(ctx, p.commands, engineSessionID, query)
		return chatSlashResult{outcome: out}, err
	case "session", "sess":
		if strings.TrimSpace(args) != "" {
			return chatSlashResult{outcome: common.SlashOutcome{
				Kind:  common.SlashHandled,
				Reply: "用法: /session",
			}}, nil
		}
		if conv == nil {
			return chatSlashResult{outcome: common.SlashOutcome{
				Kind:  common.SlashHandled,
				Reply: "当前没有活动会话，请先发送消息或执行 /new",
			}}, nil
		}
		reply, err := p.formatSessionInfo(ctx, conv)
		if err != nil {
			return chatSlashResult{}, err
		}
		return chatSlashResult{outcome: common.SlashOutcome{
			Kind:  common.SlashHandled,
			Reply: reply,
		}}, nil
	case "help", "h", "?":
		if strings.TrimSpace(args) == "" {
			return chatSlashResult{outcome: common.SlashOutcome{
				Kind:  common.SlashHandled,
				Reply: formatChatAPIHelp(p.commands),
			}}, nil
		}
	}

	out, err := common.ProcessSlash(ctx, p.commands, engineSessionID, query)
	return chatSlashResult{outcome: out}, err
}

func (p *Platform) formatSessionInfo(ctx context.Context, conv *conversation) (string, error) {
	sessionID := agentkit.SessionID(engineSessionKey(conv.ChannelKey, conv.ID))
	var b strings.Builder
	fmt.Fprintf(&b, "conversation id: %s\n", conv.ID)
	fmt.Fprintf(&b, "session id: %s\n", sessionID)
	fmt.Fprintf(&b, "turns: %d", conv.TurnCount)
	if p.sessionStore != nil {
		sess, err := p.sessionStore.Get(ctx, sessionID)
		if err == nil {
			messages, err := sess.DeriveMessages(ctx)
			if err == nil {
				fmt.Fprintf(&b, "\nmessages: %d", len(messages))
			}
		}
	}
	return strings.TrimRight(b.String(), "\n"), nil
}

func formatChatAPIHelp(commands agentkit.Commands) string {
	text := common.FormatHelp(commands)
	replacements := map[string]string{
		"start a new CLI session":                          "开始新的 conversation",
		"show current session id, path, and message count": "显示当前 conversation 和 session 信息",
	}
	for old, newText := range replacements {
		text = strings.Replace(text, old, newText, 1)
	}
	return text
}
