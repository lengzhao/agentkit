package agent

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/session/derive"
	"github.com/lengzhao/agentkit/runtime/session/sessevents"
	"golang.org/x/sync/errgroup"
)

const maxParallelToolCalls = 16

type stepControl interface {
	PopCancelReason() string
}

type toolStep struct {
	ctrl        stepControl
	toolBaseCtx context.Context
	sess        agentkit.Session
	emit        agentkit.OutboundEmit
	agentID     agentkit.AgentID
	stepIndex   int
	batch       agentkit.ToolBatchRuntime
}

func (a *Runtime) runAssistantToolCalls(
	ctx context.Context,
	sess agentkit.Session,
	emit agentkit.OutboundEmit,
	ctrl stepControl,
	toolBaseCtx context.Context,
	stepIndex int,
	agentID agentkit.AgentID,
	assistant agentkit.ModelMessage,
	calls []agentkit.ToolCall,
) error {
	if len(calls) == 0 {
		return nil
	}
	batch, ok := a.tools.(agentkit.ToolBatchRuntime)
	if !ok {
		batch = nil
	}
	step := toolStep{
		ctrl:        ctrl,
		toolBaseCtx: toolBaseCtx,
		sess:        sess,
		emit:        emit,
		agentID:     agentID,
		stepIndex:   stepIndex,
		batch:       batch,
	}
	if reason := ctrl.PopCancelReason(); reason != "" {
		_ = a.appendInterruptedToolCalls(context.WithoutCancel(ctx), sess, emit, agentID, calls, false)
		_ = sessevents.Default.AppendStepEnd(context.WithoutCancel(ctx), sess, agentID, stepIndex)
		return fmt.Errorf("cancelled: %s", reason)
	}
	for _, call := range calls {
		if err := sessevents.Default.AppendToolCall(ctx, sess, agentID, call); err != nil {
			return err
		}
	}
	if assistant.StopReason == agentkit.AssistantStopReasonLength {
		return a.persistTruncatedToolFailures(ctx, sess, emit, agentID, stepIndex, calls)
	}
	modes, err := a.toolExecutionModes(ctx)
	if err != nil {
		return err
	}
	parallel := a.toolExecution == agentkit.ToolExecutionParallel &&
		!hasSequentialToolCall(calls, modes) &&
		len(calls) >= 2 &&
		batch != nil

	var results []executedTool
	if parallel {
		results, err = a.executeToolCallsParallelPreflight(ctx, step, calls)
	} else {
		results, err = a.executeToolCallsSerial(ctx, step, calls)
	}
	if err != nil {
		return a.finishToolBatchError(ctx, sess, emit, step, calls, results, err)
	}
	for i, done := range results {
		if !done.executed {
			return fmt.Errorf("tool batch incomplete: call index %d not executed", i)
		}
		if err := a.persistToolResult(ctx, sess, emit, agentID, done.result); err != nil {
			return err
		}
	}
	return nil
}

func (a *Runtime) toolExecutionModes(ctx context.Context) (map[string]agentkit.ToolExecutionMode, error) {
	specs, err := a.tools.Visible(ctx)
	if err != nil {
		return nil, err
	}
	out := make(map[string]agentkit.ToolExecutionMode, len(specs))
	for _, spec := range specs {
		mode := spec.ExecutionMode
		if mode == "" {
			mode = agentkit.ToolExecutionParallel
		}
		out[spec.Name] = mode
	}
	return out, nil
}

func hasSequentialToolCall(calls []agentkit.ToolCall, modes map[string]agentkit.ToolExecutionMode) bool {
	for _, call := range calls {
		if modes[call.Name] == agentkit.ToolExecutionSequential {
			return true
		}
	}
	return false
}

func (a *Runtime) persistTruncatedToolFailures(
	ctx context.Context,
	sess agentkit.Session,
	emit agentkit.OutboundEmit,
	agentID agentkit.AgentID,
	stepIndex int,
	calls []agentkit.ToolCall,
) error {
	msg := "Tool call was not executed: the assistant response hit the output token limit, so arguments may be truncated. Re-issue the tool call with complete arguments."
	for _, call := range calls {
		result := agentkit.ToolResultFromExecuteError(call, errors.New(msg))
		if err := a.persistToolResult(ctx, sess, emit, agentID, result); err != nil {
			return err
		}
	}
	_ = sessevents.Default.AppendStepEnd(ctx, sess, agentID, stepIndex)
	return nil
}

func (a *Runtime) finishToolBatchError(
	ctx context.Context,
	sess agentkit.Session,
	emit agentkit.OutboundEmit,
	step toolStep,
	calls []agentkit.ToolCall,
	results []executedTool,
	err error,
) error {
	var remaining []agentkit.ToolCall
	if len(results) == len(calls) {
		for i, done := range results {
			if done.executed {
				if perr := a.persistToolResult(ctx, sess, emit, step.agentID, done.result); perr != nil {
					return perr
				}
				continue
			}
			remaining = append(remaining, calls[i])
		}
	} else {
		for _, done := range results {
			if perr := a.persistToolResult(ctx, sess, emit, step.agentID, done.result); perr != nil {
				return perr
			}
		}
		remaining = calls[len(results):]
	}
	if _, ok := cancelReasonFromError(err); ok {
		_ = a.appendInterruptedToolResults(context.WithoutCancel(ctx), sess, emit, step.agentID, remaining)
		_ = sessevents.Default.AppendStepEnd(context.WithoutCancel(ctx), sess, step.agentID, step.stepIndex)
		return err
	}
	if agentkit.IsTurnAbort(err) {
		_ = a.appendInterruptedToolResults(context.WithoutCancel(ctx), sess, emit, step.agentID, remaining)
		_ = sessevents.Default.AppendStepEnd(context.WithoutCancel(ctx), sess, step.agentID, step.stepIndex)
		return err
	}
	return err
}

type executedTool struct {
	result   agentkit.ToolResult
	executed bool
}

func (a *Runtime) executeToolCallsSerial(ctx context.Context, step toolStep, calls []agentkit.ToolCall) ([]executedTool, error) {
	out := make([]executedTool, 0, len(calls))
	for _, call := range calls {
		if err := ctx.Err(); err != nil {
			return out, err
		}
		if reason := step.ctrl.PopCancelReason(); reason != "" {
			return out, fmt.Errorf("cancelled: %s", reason)
		}
		result, err := a.invokeTool(step, call)
		if err != nil {
			return out, err
		}
		out = append(out, executedTool{result: result, executed: true})
	}
	return out, nil
}

func (a *Runtime) executeToolCallsParallelPreflight(ctx context.Context, step toolStep, calls []agentkit.ToolCall) ([]executedTool, error) {
	out := make([]executedTool, len(calls))
	var bodyIndices []int

	for i, call := range calls {
		if reason := step.ctrl.PopCancelReason(); reason != "" {
			return out, fmt.Errorf("cancelled: %s", reason)
		}
		toolCtx := withToolContext(step.toolBaseCtx, step.sess, step.agentID)
		result, call, runBody, err := step.batch.PreflightTool(toolCtx, call)
		result, err = agentkit.RecoverToolExecute(call, result, err)
		if err != nil {
			return out, err
		}
		if !runBody {
			out[i] = executedTool{result: result, executed: true}
			continue
		}
		calls[i] = call
		bodyIndices = append(bodyIndices, i)
	}

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	var abortErr error
	var abortMu sync.Mutex
	g, gctx := errgroup.WithContext(runCtx)
	sem := make(chan struct{}, maxParallelToolCalls)

	for _, idx := range bodyIndices {
		call := calls[idx]
		g.Go(func() error {
			sem <- struct{}{}
			defer func() { <-sem }()
			if gctx.Err() != nil {
				return nil
			}
			toolCtx := withToolContext(step.toolBaseCtx, step.sess, step.agentID)
			result, err := step.batch.RunToolBody(toolCtx, call)
			result, err = agentkit.RecoverToolExecute(call, result, err)
			out[idx] = executedTool{result: result, executed: true}
			if err != nil {
				if agentkit.IsTurnAbort(err) {
					abortMu.Lock()
					if abortErr == nil {
						abortErr = err
						cancel()
					}
					abortMu.Unlock()
					return err
				}
				return err
			}
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		if agentkit.IsTurnAbort(err) || agentkit.IsTurnAbort(abortErr) {
			if abortErr != nil {
				err = abortErr
			}
			return out, err
		}
		return out, err
	}
	return out, nil
}

func (a *Runtime) invokeTool(step toolStep, call agentkit.ToolCall) (agentkit.ToolResult, error) {
	toolCtx := withToolContext(step.toolBaseCtx, step.sess, step.agentID)
	if step.batch != nil {
		result, call, runBody, err := step.batch.PreflightTool(toolCtx, call)
		result, err = agentkit.RecoverToolExecute(call, result, err)
		if err != nil || !runBody {
			return result, err
		}
		result, err = step.batch.RunToolBody(toolCtx, call)
		return agentkit.RecoverToolExecute(call, result, err)
	}
	result, err := a.tools.Execute(toolCtx, call)
	return agentkit.RecoverToolExecute(call, result, err)
}

func (a *Runtime) appendInterruptedToolResults(ctx context.Context, sess agentkit.Session, emit agentkit.OutboundEmit, agentID agentkit.AgentID, calls []agentkit.ToolCall) error {
	for _, call := range calls {
		stored := derive.InterruptedToolResult(call)
		if err := sessevents.Default.AppendToolResult(ctx, sess, agentID, stored); err != nil {
			return err
		}
		if err := a.emitLifecycle(ctx, emit, agentkit.EventToolResult, stored); err != nil {
			return err
		}
	}
	return nil
}

func (a *Runtime) persistToolResult(ctx context.Context, sess agentkit.Session, emit agentkit.OutboundEmit, agentID agentkit.AgentID, result agentkit.ToolResult) error {
	stored, err := derive.PrepareToolResultForStorage(ctx, sess.ID(), result, 0)
	if err != nil {
		return err
	}
	if err := sessevents.Default.AppendToolResult(ctx, sess, agentID, stored); err != nil {
		return err
	}
	return a.emitLifecycle(ctx, emit, agentkit.EventToolResult, stored)
}
