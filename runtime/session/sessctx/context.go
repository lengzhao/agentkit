// Package sessctx prepares shared execution context for any loop or agent.
package sessctx

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"

	"github.com/lengzhao/agentkit"
	capsession "github.com/lengzhao/agentkit/cap/session"
	"github.com/lengzhao/agentkit/cap/workspace"
	"github.com/lengzhao/agentkit/runtime/rctx"
	"github.com/lengzhao/agentkit/runtime/session/sessbind"
)

// Resolve reads pending bindings and the durable context epoch without writing.
// Configuration commands use it; normal turns call Prepare.
func Resolve(ctx context.Context, store agentkit.SessionStore, ws workspace.Service) (rctx.SessionContext, error) {
	return resolve(ctx, store, ws, false)
}

// Prepare fixes the context for one turn. Runner, local and remote agents share
// this entry point; an existing snapshot prevents duplicate work. Callers must
// serialize turns for a session, as they do for all other session events.
func Prepare(ctx context.Context, store agentkit.SessionStore, ws workspace.Service) (context.Context, error) {
	// A caller without the resolver cannot complete directory preparation.
	// Leave both the snapshot and history boundary to the execution owner.
	if ws == nil {
		return ctx, nil
	}
	execution, err := resolve(ctx, store, ws, true)
	if err != nil {
		return ctx, err
	}
	return context.WithValue(rctx.WithSessionContext(ctx, execution), servicesKey{}, services{store, ws}), nil
}

func resolve(ctx context.Context, store agentkit.SessionStore, ws workspace.Service, prepare bool) (rctx.SessionContext, error) {
	sid := rctx.SessionIDFromContext(ctx)
	execution := rctx.SessionContext{SessionID: sid}
	if current, ok := rctx.SessionContextFromContext(ctx); ok && current.SessionID == sid {
		return current, nil
	}
	value, hits, err := sessbind.StackFromContext(ctx, ws).Effective(ctx, store, sessbind.CwdBinding{})
	if err != nil {
		return execution, err
	}
	// An explicit child session binding wins; channel defaults must not replace
	// the parent's already-resolved project directory during delegation.
	if inherited := rctx.InheritedWorkingDirectory(ctx); inherited != "" && hits[sessbind.LevelSession] == "" {
		value = inherited
	}
	var bindingErr error
	if value != "" {
		resolved, resolveErr := sessbind.ValidateCwd(ctx, ws, value)
		if resolveErr == nil {
			execution.Cwd = resolved
		} else {
			bindingErr = resolveErr
			// A persisted or inherited directory can disappear (repo moved,
			// temp dir cleaned, wrong host) or stop being reachable. Failing
			// here would abort every later turn of the session and even make
			// the configuration path (Resolve, behind /cwd) fail the same way,
			// leaving no way to clear the binding. Ignore it and run in the
			// consumer default instead; the explicit setters (session_cwd,
			// /cwd use) still validate and reject a bad path up front.
			slog.Warn("session context: skipping unusable cwd binding",
				"cwd", value, "err", resolveErr)
		}
	}
	if execution.Cwd == "" {
		if resolver, ok := ws.(workspace.DefaultDirectoryResolver); ok {
			dir, err := resolver.DefaultWorkingDirectory(ctx)
			if err != nil {
				return execution, err
			}
			if dir != "" {
				if bindingErr != nil {
					return execution, fmt.Errorf("invalid cwd binding %q: %w; use /cwd reset (or /cwd -c reset for channel binding)", value, bindingErr)
				}
				if prepare {
					if err = os.MkdirAll(dir, 0755); err != nil {
						return execution, fmt.Errorf("create default working directory: %w", err)
					}
				}
				execution.Cwd = dir
			}
		}
	}
	_, supportsCwd := store.(agentkit.SessionCwdStore)
	if store == nil || sid == "" || !supportsCwd {
		return execution, nil
	}
	sess, err := store.Get(ctx, sid)
	if err != nil {
		return execution, err
	}
	events, err := sess.Read(ctx, 0)
	if err != nil {
		return execution, err
	}
	previous := ""
	for i := len(events) - 1; i >= 0; i-- {
		if events[i].Type != agentkit.EventSessionContext {
			continue
		}
		var data capsession.ContextData
		if err := json.Unmarshal(events[i].Data, &data); err != nil {
			return execution, fmt.Errorf("session context: %w", err)
		}
		previous = data.Cwd
		execution.Epoch = data.Epoch
		if execution.Epoch == "" {
			execution.Epoch = fmt.Sprintf("event:%d", events[i].Seq)
		}
		break
	}
	if previous != execution.Cwd {
		// Predict the same identity for read-only configuration and the next turn.
		// Only real turns append context events; configuration cannot reset running
		// local or remote agents' model histories.
		execution.Epoch = fmt.Sprintf("%x", sha256.Sum256([]byte(execution.Epoch+"\x00"+execution.Cwd)))
		if prepare {
			data, _ := json.Marshal(capsession.ContextData{Cwd: execution.Cwd, Epoch: execution.Epoch})
			_, err = sess.Append(ctx, agentkit.SessionEvent{Type: agentkit.EventSessionContext, Data: data})
		}
	}
	return execution, err
}

type servicesKey struct{}
type services struct {
	store agentkit.SessionStore
	ws    workspace.Service
}

// NextTurn refreshes bindings for another turn dispatched by the same loop.
// Prepare stays idempotent within one turn. Custom loops draining follow-ups
// can call NextTurn before each additional turn just like loop/default.
func NextTurn(ctx context.Context) (context.Context, error) {
	deps, ok := ctx.Value(servicesKey{}).(services)
	if !ok {
		return ctx, nil
	}
	return Prepare(rctx.WithoutSessionContext(ctx), deps.store, deps.ws)
}
