package acpremote

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"syscall"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/session"

	acp "github.com/coder/acp-go-sdk"
)

func commandEnv(extra map[string]string) []string {
	env := os.Environ()
	for k, v := range extra {
		env = append(env, k+"="+v)
	}
	return env
}

// isCursorAuthError reports whether an ACP/CLI error indicates missing Cursor login.
func isCursorAuthError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "unauthenticated") ||
		strings.Contains(msg, "not logged in") ||
		strings.Contains(msg, "authenticate") ||
		strings.Contains(msg, "login") ||
		strings.Contains(msg, "peer disconnected")
}

// runCursorCLILogin blocks until Cursor CLI finishes browser OAuth.
// onOutput receives stdout/stderr lines as-is for passthrough to the chat UI.
func runCursorCLILogin(ctx context.Context, agentBin, cwd string, env map[string]string, onOutput func(string)) error {
	cmd := exec.CommandContext(ctx, agentBin, "login")
	cmd.Dir = cwd
	cmd.Env = commandEnv(env)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	cmd.Stderr = cmd.Stdout
	if err := cmd.Start(); err != nil {
		return err
	}

	reader := bufio.NewReader(stdout)
	for {
		line, readErr := reader.ReadString('\n')
		if len(line) > 0 {
			_, _ = io.WriteString(os.Stderr, line)
			if onOutput != nil {
				onOutput(line)
			}
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				break
			}
			_ = cmd.Wait()
			return readErr
		}
	}

	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("cursor login: %w", err)
	}
	slog.Info("acp-remote: cursor cli login succeeded")
	return nil
}

func (a *Runtime) runCursorLogin(ctx context.Context, emit agentkit.OutboundEmit, sessionID agentkit.SessionID) error {
	if a.cfg.AuthMethod != "cursor_login" || len(a.cfg.Command) == 0 {
		return fmt.Errorf("cursor login not configured")
	}
	agentBin := a.cfg.Command[0]

	emitter := newUpdateEmitter(ctx, sessionID, a.id, emit)
	if err := emitter.ensureStarted(); err != nil {
		return err
	}
	stream := func(line string) {
		if err := emitter.emitDelta(agentkit.AssistantEventTextDelta, 0, line); err != nil {
			slog.Debug("acp-remote: emit login output failed", "err", err)
		}
	}

	cwd, err := a.bridge.resolveCwd(ctx)
	if err != nil {
		return err
	}

	slog.Info("acp-remote: waiting for cursor cli login")
	err = runCursorCLILogin(ctx, agentBin, cwd, a.cfg.Env, stream)
	if ferr := emitter.finalize(); ferr != nil && err == nil {
		err = ferr
	}
	if err != nil {
		return err
	}
	if a.sessionStore != nil {
		sess, err := a.sessionStore.Get(ctx, sessionID)
		if err != nil {
			return err
		}
		assistant := emitter.assistantMessage()
		if err := session.AppendMessage(ctx, sess, a.id, agentkit.EventAssistantMessage, assistant); err != nil {
			slog.Debug("acp-remote: append assistant message failed", "err", err)
		}
	}
	return nil
}

// ensureACPSessionWithAuth tries ACP first; on auth failure runs agent login then retries once.
func (a *Runtime) ensureACPSessionWithAuth(ctx context.Context, emit agentkit.OutboundEmit, sessionID agentkit.SessionID) (acp.SessionId, error) {
	acpSessionID, err := a.bridge.ensureACPSession(ctx, sessionID)
	if err == nil {
		return acpSessionID, nil
	}
	if a.cfg.AuthMethod != "cursor_login" || !isCursorAuthError(err) {
		return "", err
	}

	slog.Info("acp-remote: acp auth failed, starting cursor login", "err", err)
	_ = a.bridge.Close()

	if loginErr := a.runCursorLogin(ctx, emit, sessionID); loginErr != nil {
		return "", loginErr
	}

	return a.bridge.ensureACPSession(ctx, sessionID)
}
