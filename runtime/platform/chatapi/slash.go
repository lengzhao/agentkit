package chatapi

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/platform/common"
)

type chatSlashResult struct {
	outcome              common.SlashOutcome
	switchConversationID string
}

func isNewSlash(query string) bool {
	name, args, ok := common.ParseSlashCommand(query)
	return ok && name == "new" && strings.TrimSpace(args) == ""
}

func (p *Platform) serveNewConversation(w http.ResponseWriter, r *http.Request, channelKey, user string, requestAgentID agentkit.AgentID) {
	conv, err := p.createConversation(r.Context(), channelKey, user)
	if err != nil {
		slog.Error("chat-api: create conversation", "err", err)
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	conv.bindAgent(requestAgentID)
	p.persistConversationIndex(r.Context(), channelKey)

	sse, err := newSSEWriter(w)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}

	runID := newRunID()
	engineSessionKey := engineSessionKey(channelKey, conv.ID)
	msgID := messageID(conv.ID, 0)
	inboundAgentID := p.resolveInboundAgentID(requestAgentID, conv)
	run := newRunState(runID, user, channelKey, inboundAgentID, agentkit.SessionID(engineSessionKey), conv.ID, msgID, p, sse)
	if !p.pending.create(run) {
		_ = sse.Error("too many concurrent requests")
		return
	}
	p.setActiveConv(conv.ID, runID)

	if err := sse.Event("message", map[string]any{
		"conversation_id": conv.ID,
		"message_id":      msgID,
		"run_id":          runID,
	}); err != nil {
		return
	}

	reply := fmt.Sprintf("已开始新会话：%s", conv.ID)
	run.mu.Lock()
	run.answerText = reply
	run.mu.Unlock()
	_ = run.flushDeltas()
	p.pending.finish(runID, pendingResult{answer: reply})
	p.clearActiveConv(conv.ID, runID)
}

func (p *Platform) processChatSlash(ctx context.Context, channelKey string, conv *conversation, engineSessionID agentkit.SessionID, query string) (chatSlashResult, error) {
	name, args, ok := common.ParseSlashCommand(query)
	if !ok {
		out, err := common.ProcessSlash(ctx, p.commands, engineSessionID, query)
		return chatSlashResult{outcome: out}, err
	}

	switch name {
	case "new":
		if strings.TrimSpace(args) != "" {
			return chatSlashResult{outcome: common.SlashOutcome{
				Kind:  common.SlashHandled,
				Reply: "用法: /new",
			}}, nil
		}
		createdBy := ""
		if conv != nil {
			createdBy = conv.CreatedBy
		}
		newConv, err := p.createConversation(ctx, channelKey, createdBy)
		if err != nil {
			return chatSlashResult{}, err
		}
		if conv != nil {
			newConv.bindAgent(conv.agentID())
		}
		p.persistConversationIndex(ctx, channelKey)
		return chatSlashResult{
			outcome: common.SlashOutcome{
				Kind:  common.SlashHandled,
				Reply: fmt.Sprintf("已开始新会话：%s", newConv.ID),
			},
			switchConversationID: newConv.ID,
		}, nil
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
		"start a new CLI session":        "开始新的 conversation",
		"show current session id, path, and message count": "显示当前 conversation 和 session 信息",
	}
	for old, newText := range replacements {
		text = strings.Replace(text, old, newText, 1)
	}
	return text
}
