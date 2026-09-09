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

func (p *Platform) slashContext(delivery agentkit.SessionID, conv *conversation, user string, metadata map[string]any) common.SlashContext {
	ctx := common.SlashContext{
		Route: session.BuildSessionRoute(session.SessionRouteInput{
			Platform:    "chat-api",
			DeliveryID:  delivery,
			ScopeUserID: user,
		}),
		SessionScope: p.sessionScope,
		UserID:       user,
	}
	if conv != nil || len(metadata) > 0 {
		ctx.Metadata = mergeSlashMetadata(metadata, conv)
	}
	return ctx
}

func mergeSlashMetadata(metadata map[string]any, conv *conversation) map[string]any {
	var out map[string]any
	if len(metadata) > 0 {
		out = make(map[string]any, len(metadata)+2)
		for k, v := range metadata {
			out[k] = v
		}
	}
	if conv != nil {
		if out == nil {
			out = make(map[string]any, 2)
		}
		out[session.MetadataConversationID] = conv.ID
		out[session.MetadataTurnCount] = conv.TurnCount
	}
	return out
}

func (p *Platform) processChatSlash(ctx context.Context, _ string, conv *conversation, engineSessionID agentkit.SessionID, user string, metadata map[string]any, query string) (chatSlashResult, error) {
	name, args, ok := common.ParseSlashCommand(query)
	if ok && name == "help" && strings.TrimSpace(args) == "" {
		slash := p.slashContext(engineSessionID, conv, user, metadata)
		cmdCtx := common.SlashCommandContext(ctx, p.commands, slash)
		return chatSlashResult{outcome: common.SlashOutcome{
			Kind:  common.SlashHandled,
			Reply: formatChatAPIHelp(cmdCtx, p.commands),
		}}, nil
	}
	out, err := common.ProcessSlash(ctx, p.commands, p.slashContext(engineSessionID, conv, user, metadata), query)
	return chatSlashResult{outcome: out}, err
}

func formatChatAPIHelp(ctx context.Context, commands agentkit.Commands) string {
	text := common.FormatHelp(ctx, commands)
	replacements := map[string]string{
		"start a new conversation session":                 "开始新的 conversation",
		"show current session id, path, and message count": "显示当前 conversation 和 session 信息",
	}
	for old, newText := range replacements {
		text = strings.Replace(text, old, newText, 1)
	}
	return text
}
