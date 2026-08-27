package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/compaction"
	"github.com/lengzhao/agentkit/runtime/session"
)

type Config struct {
	// ID is agent id, referenced by loop.defaultAgent.
	ID agentkit.AgentID `json:"id"`
	// Model is model name passed to the LLM provider.
	Model string `json:"model"`
	// MaxSteps is steps allowed in one segment.
	MaxSteps int `json:"maxSteps"`
	// Retry is per-step retry for transient provider failures.
	Retry *RetryConfig `json:"retry,omitempty"`
	// Budget is hard bounds for a whole turn including its continuations. No hook can extend a turn past these.
	Budget *BudgetConfig `json:"budget,omitempty"`
}

type Deps struct {
	SessionStore agentkit.SessionStore    `json:"sessionStore"`
	LLM          agentkit.LLMProvider     `json:"llm"`
	Tools        agentkit.ToolRuntime     `json:"tools"`
	Prompt       agentkit.PromptAssembler `json:"prompt"`
	Policies     []agentkit.Policy        `json:"policies,omitempty"`
	Hooks        agentkit.HookRuntime     `json:"hooks,omitempty"`
	Compaction   []compaction.Service     `json:"compaction,omitempty"`
}

type Runtime struct {
	id           agentkit.AgentID
	model        string
	maxSteps     int
	retry        retrySettings
	budget       budgetSettings
	now          func() time.Time
	sessionStore agentkit.SessionStore
	llm          agentkit.LLMProvider
	tools        agentkit.ToolRuntime
	prompt       agentkit.PromptAssembler
	hooks        agentkit.HookRuntime
	compaction   []compaction.Service
}

// New registers agent/coding: Default coding agent: runs one turn against session, LLM, tools and prompt.
//
// Best practices:
//   - budget.maxContinuations defaults to 0, i.e. one segment: an agent stays request/response until you raise it.
//   - budget.softRatio (default 0.8) marks the point where a turn-stopping hook should wrap up rather than start new work.
//   - An interrupted turn is repaired on the next turn, so a crash mid-tool-call does not leave the session unusable.
func New(cfg Config, deps Deps) (agentkit.Agent, error) {
	id := cfg.ID
	if id == "" {
		id = "coding"
	}
	maxSteps := cfg.MaxSteps
	if maxSteps <= 0 {
		maxSteps = 20
	}
	if deps.SessionStore == nil {
		return nil, fmt.Errorf("agent requires sessionStore")
	}
	if deps.LLM == nil {
		return nil, fmt.Errorf("agent requires llm")
	}
	if deps.Tools == nil {
		return nil, fmt.Errorf("agent requires tools runtime")
	}
	if deps.Prompt == nil {
		return nil, fmt.Errorf("agent requires prompt assembler")
	}
	return &Runtime{
		id:           id,
		model:        cfg.Model,
		maxSteps:     maxSteps,
		retry:        resolveRetrySettings(cfg.Retry),
		budget:       resolveBudgetSettings(cfg.Budget),
		now:          time.Now,
		sessionStore: deps.SessionStore,
		llm:          deps.LLM,
		tools:        deps.Tools,
		prompt:       deps.Prompt,
		hooks:        deps.Hooks,
		compaction:   deps.Compaction,
	}, nil
}

func (a *Runtime) ID() agentkit.AgentID { return a.id }

// turnRun holds mutable state for one turn, spanning every segment the
// TurnStopping hooks extend it with.
type turnRun struct {
	budget    *runBudget
	completed int
}

func (a *Runtime) RunTurn(ctx context.Context, input agentkit.TurnInput) error {
	sessionID, ok := ctx.Value(agentkit.KeySessionID).(agentkit.SessionID)
	if !ok || sessionID == "" {
		return fmt.Errorf("turn requires session id in context")
	}
	sess, err := a.sessionStore.Get(ctx, sessionID)
	if err != nil {
		return err
	}
	ctrl := turnControlFrom(ctx)

	ctrl.ClearTurnCancel()

	// A previous process may have died mid-turn. Close it out before starting a
	// new one, so this turn builds on a replayable history.
	if err := a.recoverIncompleteTurn(ctx, sess, input.Emit); err != nil {
		return err
	}

	run := &turnRun{budget: newRunBudget(a.budget, a.now)}
	if err := a.emitLifecycle(ctx, input.Emit, sessionID, agentkit.EventTurnStart, session.TurnStartData{}); err != nil {
		return err
	}
	if err := session.AppendTurnStart(ctx, sess, a.id); err != nil {
		return err
	}
	defer func() {
		endCtx := context.WithoutCancel(ctx)
		_ = session.AppendTurnEnd(endCtx, sess, a.id, run.completed)
		if err := a.emitLifecycle(endCtx, input.Emit, sessionID, agentkit.EventTurnEnd, session.TurnEndData{Steps: run.completed}); err != nil {
			slog.Debug("agent: emit turn/end failed", "agent_id", a.id, "session_id", sessionID, "err", err)
		}
	}()

	if err := session.AppendMessage(ctx, sess, a.id, agentkit.EventUserMessage, input.Message); err != nil {
		return err
	}

	for {
		reason, err := a.runSegment(ctx, sess, input.Emit, ctrl, run)
		if err != nil {
			return err
		}
		extended, err := a.extendTurn(ctx, sess, input.Emit, run, reason)
		if err != nil {
			return err
		}
		if !extended {
			return nil
		}
	}
}

// runSegment drives model steps until the assistant stops requesting tools or
// the segment's step allowance runs out, and reports why it stopped.
func (a *Runtime) runSegment(
	ctx context.Context,
	sess agentkit.Session,
	emit agentkit.OutboundEmit,
	ctrl turnControl,
	run *turnRun,
) (agentkit.TurnStopReason, error) {
	maxSteps := run.budget.stepsForSegment(a.maxSteps)
	if maxSteps <= 0 {
		return agentkit.StopBudget, nil
	}

	// Overflow recovery is per segment: a long autonomous run must be able to
	// compact again after the first recovery.
	overflowRecoveryAttempted := false

	for step := 0; step < maxSteps; step++ {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		if reason := ctrl.PopCancelReason(); reason != "" {
			return "", fmt.Errorf("cancelled: %s", reason)
		}

		for _, msg := range ctrl.PopSteering() {
			if err := session.AppendMessage(ctx, sess, a.id, agentkit.EventUserMessage, msg); err != nil {
				return "", err
			}
		}

		run.budget.recordStep()
		stepIndex := run.budget.stepsUsed() - 1

		stepCtx, endStep := ctrl.BeginStep(ctx)
		stepDone := false
		endStepOnce := func() {
			if stepDone {
				return
			}
			stepDone = true
			endStep()
		}

		if err := session.AppendStepStart(ctx, sess, a.id, stepIndex); err != nil {
			endStepOnce()
			return "", err
		}

		stepRetry := newStepRetry(a.retry)

		outcome, err := a.runStepWithOverflowRecovery(stepCtx, sess, emit, stepRetry, &overflowRecoveryAttempted)
		if err != nil {
			if ctrl.ShouldContinueAfterInterrupt(ctx, stepCtx, err) {
				_ = session.AppendStepEnd(context.WithoutCancel(ctx), sess, a.id, stepIndex)
				endStepOnce()
				continue
			}
			_ = session.AppendStepEnd(context.WithoutCancel(ctx), sess, a.id, stepIndex)
			endStepOnce()
			return "", err
		}

		if err := a.recordUsage(ctx, sess, run, outcome.usage); err != nil {
			endStepOnce()
			return "", err
		}

		assistant := outcome.message
		toolInterrupted := false
		for _, call := range assistant.ToolCalls {
			if err := session.AppendToolCall(ctx, sess, a.id, call); err != nil {
				_ = session.AppendStepEnd(context.WithoutCancel(ctx), sess, a.id, stepIndex)
				endStepOnce()
				return "", err
			}
			toolCtx := withToolContext(stepCtx, sess.ID(), a.id)
			result, err := a.tools.Execute(toolCtx, call)
			if err != nil {
				if ctrl.ShouldContinueAfterInterrupt(ctx, stepCtx, err) {
					toolInterrupted = true
					break
				}
				_ = session.AppendStepEnd(context.WithoutCancel(ctx), sess, a.id, stepIndex)
				endStepOnce()
				return "", err
			}
			if err := session.AppendToolResult(ctx, sess, a.id, result); err != nil {
				_ = session.AppendStepEnd(context.WithoutCancel(ctx), sess, a.id, stepIndex)
				endStepOnce()
				return "", err
			}
		}

		if err := session.AppendStepEnd(ctx, sess, a.id, stepIndex); err != nil {
			endStepOnce()
			return "", err
		}
		run.completed++
		endStepOnce()

		if toolInterrupted {
			continue
		}

		if len(assistant.ToolCalls) == 0 {
			return agentkit.StopNoToolCalls, nil
		}
	}

	return agentkit.StopStepLimit, nil
}

// extendTurn consults TurnStopping hooks. When they ask to keep going and the
// hard budget still allows it, the continuation is recorded as a turn/continue
// event, which derive replays as a user message for the next segment.
func (a *Runtime) extendTurn(
	ctx context.Context,
	sess agentkit.Session,
	emit agentkit.OutboundEmit,
	run *turnRun,
	reason agentkit.TurnStopReason,
) (bool, error) {
	if a.hooks == nil {
		return false, nil
	}
	state := run.budget.state()
	if state.Exhausted {
		reason = agentkit.StopBudget
	}
	messages, err := sess.DeriveMessages(ctx)
	if err != nil {
		return false, err
	}
	stopping := &agentkit.TurnStopping{
		Reason:   reason,
		Steps:    run.budget.stepsUsed(),
		Segments: run.budget.continuationsUsed(),
		Budget:   state,
		Messages: messages,
	}
	if err := a.hooks.TurnStopping(ctx, stopping); err != nil {
		return false, err
	}

	if stopping.Stop || len(stopping.Continue) == 0 {
		if stopping.StopReason != "" {
			slog.Info("turn stopping",
				"agent_id", a.id,
				"session_id", sess.ID(),
				"reason", string(reason),
				"stop_reason", stopping.StopReason,
				"steps", run.budget.stepsUsed(),
				"continuations", run.budget.continuationsUsed(),
			)
		}
		return false, nil
	}

	// The hard budget wins: no hook can extend a turn past it.
	if !run.budget.allowsContinuation() {
		slog.Info("turn continuation denied by budget",
			"agent_id", a.id,
			"session_id", sess.ID(),
			"steps", run.budget.stepsUsed(),
			"continuations", run.budget.continuationsUsed(),
			"tokens", run.budget.tokensUsed(),
		)
		return false, nil
	}

	run.budget.recordContinuation()
	data := session.TurnContinueData{
		Segment:  run.budget.continuationsUsed(),
		Reason:   string(reason),
		Steps:    run.budget.stepsUsed(),
		Messages: stopping.Continue,
	}
	if err := session.AppendTurnContinue(ctx, sess, a.id, data); err != nil {
		return false, err
	}
	slog.Info("turn continued",
		"agent_id", a.id,
		"session_id", sess.ID(),
		"segment", data.Segment,
		"reason", data.Reason,
		"steps", data.Steps,
	)
	if emit != nil {
		if err := emit(ctx, agentkit.OutboundEvent{
			SessionID: sess.ID(),
			AgentID:   a.id,
			Type:      agentkit.EventTurnContinue,
			Data:      agentkit.MarshalOutboundData(data),
		}); err != nil {
			return false, err
		}
	}
	return true, nil
}

// recordUsage charges the run budget and logs token accounting for one step.
func (a *Runtime) recordUsage(ctx context.Context, sess agentkit.Session, run *turnRun, usage *agentkit.Usage) error {
	if usage == nil {
		return nil
	}
	total := usage.TotalTokens
	if total == 0 {
		total = usage.InputTokens + usage.OutputTokens
	}
	if total == 0 {
		return nil
	}
	run.budget.recordTokens(total)
	return session.AppendUsage(ctx, sess, a.id, session.UsageData{
		InputTokens:  usage.InputTokens,
		OutputTokens: usage.OutputTokens,
		TotalTokens:  total,
	})
}

// stepOutcome is one model step's result: the assistant message plus whatever
// token accounting the provider reported.
type stepOutcome struct {
	message agentkit.ModelMessage
	usage   *agentkit.Usage
}

func (a *Runtime) runStep(ctx context.Context, sess agentkit.Session, emit agentkit.OutboundEmit) (stepOutcome, error) {
	history, err := a.prepareStepHistory(ctx, sess)
	if err != nil {
		return stepOutcome{}, err
	}
	specs, err := a.tools.Visible(ctx)
	if err != nil {
		return stepOutcome{}, err
	}
	messages, err := a.prompt.Assemble(ctx, agentkit.PromptRequest{
		Messages: history,
	})
	if err != nil {
		return stepOutcome{}, err
	}

	stream, err := a.llm.Stream(ctx, agentkit.LLMRequest{
		Model:    a.model,
		Messages: messages,
		Tools:    specs,
	})
	if err != nil {
		return stepOutcome{}, err
	}
	defer stream.Close()

	streamOut := newStreamEmitter(ctx, sess.ID(), a.id, emit)

	var assistant agentkit.ModelMessage
	var usage *agentkit.Usage
	for {
		if err := ctx.Err(); err != nil {
			return stepOutcome{}, err
		}
		ev, err := stream.Recv()
		if ev.Message != nil {
			assistant = *ev.Message
		}
		if ev.Usage != nil {
			usage = ev.Usage
		}
		if consumeErr := streamOut.consume(ev); consumeErr != nil {
			return stepOutcome{}, consumeErr
		}
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return stepOutcome{}, err
		}
	}
	if assistant.Role == "" {
		assistant.Role = "assistant"
	}
	if err := streamOut.finalize(assistant); err != nil {
		return stepOutcome{}, err
	}

	if err := session.AppendMessage(ctx, sess, a.id, agentkit.EventAssistantMessage, assistant); err != nil {
		return stepOutcome{}, err
	}
	slog.Info("assistant step", "agent_id", a.id, "session_id", sess.ID(), "tool_calls", len(assistant.ToolCalls))
	return stepOutcome{message: assistant, usage: usage}, nil
}

func (a *Runtime) prepareStepHistory(ctx context.Context, sess agentkit.Session) ([]agentkit.ModelMessage, error) {
	history, err := sess.DeriveMessages(ctx)
	if err != nil {
		return nil, err
	}
	if a.hooks == nil {
		return history, nil
	}
	step := &agentkit.BeforeStep{Messages: history}
	if err := a.hooks.BeforeStep(ctx, step); err != nil {
		return nil, err
	}
	if step.Messages != nil {
		return step.Messages, nil
	}
	return sess.DeriveMessages(ctx)
}

func withToolContext(ctx context.Context, sessionID agentkit.SessionID, agentID agentkit.AgentID) context.Context {
	ctx = context.WithValue(ctx, agentkit.KeySessionID, sessionID)
	ctx = context.WithValue(ctx, agentkit.KeyAgentID, agentID)
	return ctx
}

func EncodeEventData(v any) json.RawMessage {
	raw, _ := json.Marshal(v)
	return raw
}

func (a *Runtime) emitLifecycle(ctx context.Context, emit agentkit.OutboundEmit, sessionID agentkit.SessionID, typ agentkit.EventType, data any) error {
	if emit == nil {
		return nil
	}
	return emit(ctx, agentkit.OutboundEvent{
		SessionID: sessionID,
		AgentID:   a.id,
		Type:      typ,
		Data:      agentkit.MarshalOutboundData(data),
	})
}
