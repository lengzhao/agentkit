package acpremote

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"strings"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/rctx"
)

// authGuidanceError exposes a safe, actionable message while retaining the
// protocol error for errors.Is/As. Never render protocol Data or secret values.
type authGuidanceError struct {
	cause error
	hint  string
}

func (e *authGuidanceError) Error() string {
	detail := acpErrorDetail(e.cause)
	if detail == "" {
		return e.hint
	}
	return e.hint + "\n\n错误详情：" + detail
}
func (e *authGuidanceError) Unwrap() error { return e.cause }

func isACPAuthenticationError(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	if re := requestErrorFromChain(err); re != nil && re.Code == -32000 {
		return true
	}
	// Compatibility with adapters which flatten the protocol error to text.
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "authentication required") ||
		strings.Contains(text, "authentication failed") ||
		strings.Contains(text, "not authenticated") ||
		strings.Contains(text, `"code":-32000`) ||
		strings.Contains(text, "code -32000")
}

func (a *Runtime) authHintForError(err error) string {
	if !isACPAuthenticationError(err) {
		return ""
	}
	hint := strings.TrimSpace(a.cfg.AuthHint)
	if hint == "" {
		hint = "Agent 认证失败，当前请求没有获得可用凭据。请先在同一系统用户的终端确认对应 CLI 已登录，或配置该 Agent 支持的凭据。\n如果凭据写在 shell 启动文件（如 ~/.zshrc），请确认文件及父目录可读，并使用 export 导出变量；修改后重启启动该 Agent 的后台服务，再重试。终端可用但后台失败时，应检查二者的用户身份、HOME 和环境变量是否一致。"
	}
	keys := authEnvironmentKeys(a.cfg)
	if len(keys) > 0 {
		states := make([]string, 0, len(keys))
		for _, key := range keys {
			value, configured := a.cfg.Env[key]
			if !configured {
				value = os.Getenv(key)
			}
			state := "未设置"
			if value != "" {
				state = "已设置"
			}
			states = append(states, key+"="+state)
		}
		hint += "\nACP 子进程环境（仅显示是否设置，不代表凭据有效）：" + strings.Join(states, "，")
	}
	return hint
}

func authEnvironmentKeys(cfg Config) []string {
	identity := strings.ToLower(string(cfg.ID) + " " + strings.Join(cfg.Command, " "))
	switch {
	case strings.Contains(identity, "claude"):
		return []string{"ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN", "CLAUDE_CODE_OAUTH_TOKEN", "ANTHROPIC_BASE_URL"}
	case strings.Contains(identity, "codex"):
		return []string{"CODEX_API_KEY", "OPENAI_API_KEY"}
	case strings.EqualFold(string(cfg.ID), "cursor") || (len(cfg.Command) > 0 && cfg.Command[0] == "agent"):
		return []string{"CURSOR_API_KEY"}
	default:
		return nil
	}
}

func (a *Runtime) withAuthGuidance(ctx context.Context, err error) error {
	if err == nil {
		return nil
	}
	sessionID := rctx.SessionIDFromContext(ctx)
	attrs := []any{
		"agent_id", a.id,
		"session_id", sessionID,
		"command", strings.Join(a.cfg.Command, " "),
		"stage", acpErrorStage(err),
		"detail", acpErrorDetail(err),
		"err", err,
	}
	if re := requestErrorFromChain(err); re != nil {
		attrs = append(attrs, "acp_code", re.Code, "acp_message", re.Message)
	}
	if hint := a.authHintForError(err); hint != "" {
		slog.Warn("acp-remote: authentication required", attrs...)
		return &authGuidanceError{cause: err, hint: hint}
	}
	slog.Error("acp-remote: turn failed", attrs...)
	return err
}

func logACPRemoteError(agentID agentkit.AgentID, cfg Config, sessionID agentkit.SessionID, stage string, err error) {
	if err == nil {
		return
	}
	attrs := []any{
		"agent_id", agentID,
		"session_id", sessionID,
		"command", strings.Join(cfg.Command, " "),
		"stage", stage,
		"detail", acpErrorDetail(err),
		"err", err,
	}
	if re := requestErrorFromChain(err); re != nil {
		attrs = append(attrs, "acp_code", re.Code, "acp_message", re.Message)
	}
	slog.Warn("acp-remote: "+stage+" failed", attrs...)
}

// Compile-time assurance that wrapping continues to satisfy the error contract.
var _ error = (*authGuidanceError)(nil)
