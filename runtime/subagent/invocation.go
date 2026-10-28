package subagent

import (
	"context"
	"errors"
	"time"

	"github.com/lengzhao/agentkit"
	capsession "github.com/lengzhao/agentkit/cap/session"
	"github.com/lengzhao/agentkit/cap/subagent"
	"github.com/lengzhao/agentkit/cap/workspace"
	"github.com/lengzhao/agentkit/runtime/execution"
	"github.com/lengzhao/agentkit/runtime/rctx"
	"github.com/lengzhao/agentkit/runtime/session/derive"
	"github.com/lengzhao/agentkit/runtime/session/sessevents"
)

// childInvocation pins the already-resolved target and complete task. The
// executor never re-resolves a name or substitutes a selector's rewritten task.
type childInvocation struct {
	Name      string
	Agent     agentkit.Agent
	SessionID agentkit.SessionID
	Message   agentkit.ModelMessage
	After     agentkit.EventSeq
}

// Compatibility policies belong to existing backend adapters, not role identity
// or model arguments. New hosts should use strict scoped synchronous execution.
type childContextPolicy struct {
	asyncParentControl bool
}

func prepareChildContext(parent context.Context, sessionID agentkit.SessionID, agentID agentkit.AgentID, wall time.Duration, policy childContextPolicy) (context.Context, func(), error) {
	// Existing Spawner callers use the same legacy workspace resolver as the
	// rest of the runtime. Normalize that trusted context here; the new scope
	// API stays strict and has no route-specific compatibility switches.
	if rctx.EnvelopeFromContext(parent).Workspace == "" {
		parent = rctx.WithWorkspace(parent, rctx.WorkspaceFromContext(parent))
	}
	endDeadline := func() {}
	if wall > 0 {
		parent, endDeadline = context.WithTimeout(parent, wall)
	}
	var ctx context.Context
	var closeScope context.CancelFunc
	if parent.Value(agentkit.KeyAsyncSubagent) == nil {
		var err error
		ctx, closeScope, err = execution.NewChild(parent, execution.ChildOptions{HistorySessionID: sessionID, AgentID: agentID})
		if err != nil {
			endDeadline()
			return nil, nil, err
		}
	} else {
		// Compatibility only: a real background-job owner will replace this
		// policy. Identity can be shared without sharing synchronous ownership.
		ctx = rctx.WithExecutionIdentity(parent, execution.NewIdentity(parent))
		ctx = rctx.WithInheritedWorkingDirectory(ctx, workspace.WorkingDirectory(parent))
		ctx = rctx.WithoutSessionContext(ctx)
		ctx = workspace.WithWorkingDirectory(ctx, workspace.WorkingDirectory(parent))
		ctx = rctx.ApplyEnvelopeToContext(ctx, rctx.EnvelopeFromContext(parent).WithConversation(string(sessionID)).WithAgentID(agentID))
		ctx = context.WithValue(ctx, agentkit.KeySession, struct{}{})
		ctx = context.WithValue(ctx, agentkit.KeyInSubagent, true)
		if !policy.asyncParentControl {
			ctx = context.WithValue(ctx, agentkit.KeySessionControl, struct{}{})
		}
		closeScope = func() {}
	}
	return ctx, func() { closeScope(); endDeadline() }, nil
}

func taskMessage(task string) agentkit.ModelMessage {
	return agentkit.ModelMessage{Role: "user", Content: []agentkit.ContentPart{{Type: "text", Text: task}}}
}

// invokeChild is shared by local and configured-agent backends. Backends own
// construction/telemetry; history projection and output forwarding live here.
// The caller owns the returned async emitter drain, including on run failure.
func invokeChild(parent, ctx context.Context, store agentkit.SessionStore, in childInvocation) (subagent.Result, error, func()) {
	out := subagent.Result{Agent: in.Name, Session: string(in.SessionID)}
	emit, closer := forwardParentEmit(parent, emitFromContext(parent))
	runErr := ctx.Err()
	if runErr == nil {
		message := in.Message
		message.Content = append([]agentkit.ContentPart(nil), message.Content...)
		runErr = in.Agent.RunTurn(ctx, agentkit.TurnInput{Message: message, Emit: emit})
	}
	// Read partial output after backend failure, using the parent's store scope.
	sess, err := sessevents.LoadSession(parent, store, in.SessionID)
	if err != nil {
		if runErr == nil {
			return out, err, closer
		}
		return out, errors.Join(runErr, err), closer
	}
	events, err := derive.ReadAllEvents(parent, sess)
	if err != nil {
		if runErr == nil {
			return out, err, closer
		}
		return out, errors.Join(runErr, err), closer
	}
	out.Steps = capsession.StepCount(events, in.After)
	if finish := capsession.FinishAfter(events, in.After); finish != nil {
		out.Status, out.Summary = finish.Status, finish.Summary
	} else {
		out.Status, out.Summary = subagent.StatusStopped, capsession.LastAssistantText(events, in.After)
	}
	return out, runErr, closer
}
