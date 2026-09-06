package chatapi

import (
	"context"
	"strings"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/platform/common"
	"github.com/lengzhao/agentkit/runtime/session"
)

type chatSlashResult struct {
	outcome common.SlashOutcome
}

func (p *Platform) slashContext(delivery agentkit.SessionID, conv *conversation) common.SlashContext {
	ctx := common.SlashContext{
		Route:        session.SessionRouteFromDelivery("chat-api", delivery, ""),
		SessionScope: p.sessionScope,
	}
	if conv != nil {
		ctx.Metadata = map[string]any{
			session.MetadataConversationID: conv.ID,
			session.MetadataTurnCount:      conv.TurnCount,
		}
	}
	return ctx
}

func (p *Platform) processChatSlash(ctx context.Context, _ string, conv *conversation, engineSessionID agentkit.SessionID, query string) (chatSlashResult, error) {
	name, args, ok := common.ParseSlashCommand(query)
	if ok && name == "help" && strings.TrimSpace(args) == "" {
		return chatSlashResult{outcome: common.SlashOutcome{
			Kind:  common.SlashHandled,
			Reply: formatChatAPIHelp(p.commands),
		}}, nil
	}
	out, err := common.ProcessSlash(ctx, p.commands, p.slashContext(engineSessionID, conv), query)
	return chatSlashResult{outcome: out}, err
}

func formatChatAPIHelp(commands agentkit.Commands) string {
	text := common.FormatHelp(commands)
	replacements := map[string]string{
		"start a new conversation session":                 "开始新的 conversation",
		"show current session id, path, and message count": "显示当前 conversation 和 session 信息",
	}
	for old, newText := range replacements {
		text = strings.Replace(text, old, newText, 1)
	}
	return text
}
