package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/compaction"
	capsession "github.com/lengzhao/agentkit/cap/session"
	captelemetry "github.com/lengzhao/agentkit/cap/telemetry"
	"github.com/lengzhao/agentkit/cap/workspace"
	rtcompaction "github.com/lengzhao/agentkit/runtime/compaction"
	rtllm "github.com/lengzhao/agentkit/runtime/llm"
	"github.com/lengzhao/agentkit/runtime/rctx"
	"github.com/lengzhao/agentkit/runtime/session/derive"
	"github.com/lengzhao/agentkit/runtime/session/sessbind"
	"github.com/lengzhao/agentkit/runtime/session/sessevents"
	"github.com/lengzhao/agentkit/runtime/telemetry"
)

type Config struct {
	// ID is agent id, referenced by loop.defaultAgent.
	ID agentkit.AgentID `json:"id"`
	// Model is model name passed to the LLM provider.
	Model string `json:"model"`
	// Modalities overrides provider input modalities when set (e.g. vision subagent with modalities: [image]).
	Modalities []string `json:"modalities,omitempty"`
	// Retry is per-step retry for transient provider failures.
	Retry *RetryConfig `json:"retry,omitempty"`
	// MaxSteps caps model steps per turn (all segments). Omitted defaults to 200;
	// explicit 0 disables the cap.
	MaxSteps *int `json:"maxSteps,omitempty"`
	// MaxPromptTokens is the pre-send guard: when the assembled prompt (system
	// prompt + hydrated history, inline media counted at real size) exceeds it,
	// force-run compaction once before the LLM call; if it still exceeds, fail
	// the step instead of sending a request the provider will reject. 0 disables.
	MaxPromptTokens int `json:"maxPromptTokens,omitempty"`
}

type Deps struct {
	SessionStore agentkit.SessionStore    `json:"sessionStore"`
	LLM          agentkit.LLMProvider     `json:"llm"`
	Tools        agentkit.ToolRuntime     `json:"tools"`
	Prompt       agentkit.PromptAssembler `json:"prompt"`
	Policies     []agentkit.Policy        `json:"policies,omitempty"`
	Hooks        agentkit.HookRuntime     `json:"hooks,omitempty"`
	Compaction   []compaction.Service     `json:"compaction,omitempty"`
	Workspace    workspace.Service        `json:"workspace,omitempty"`
}

type Runtime struct {
	id              agentkit.AgentID
	model           string
	modalities      []string
	retry           retrySettings
	maxSteps        int
	maxPromptTokens int
	now             func() time.Time
	sessionStore    agentkit.SessionStore
	llm             agentkit.LLMProvider
	tools           agentkit.ToolRuntime
	prompt          agentkit.PromptAssembler
	hooks           agentkit.HookRuntime
	compaction      []compaction.Service
	workspace       workspace.Service
}

// New registers agent/coding: Default coding agent: runs one turn against session, LLM, tools and prompt.
//
// Best practices:
//   - An interrupted turn is repaired on the next turn, so a crash mid-tool-call does not leave the session unusable.
//   - Autonomous continuation belongs in TurnStopping hooks (e.g. hook/turn-continue); per-turn step caps use maxSteps.
func New(cfg Config, deps Deps) (agentkit.Agent, error) {
	id := cfg.ID
	if id == "" {
		id = "coding"
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
	if deps.Workspace == nil {
		return nil, fmt.Errorf("agent requires workspace")
	}
	return &Runtime{
		id:              id,
		model:           cfg.Model,
		modalities:      agentkit.NormalizeModalities(cfg.Modalities),
		retry:           resolveRetrySettings(cfg.Retry),
		maxSteps:        resolveMaxSteps(cfg.MaxSteps),
		maxPromptTokens: cfg.MaxPromptTokens,
		now:             time.Now,
		sessionStore:    deps.SessionStore,
		llm:             deps.LLM,
		tools:           deps.Tools,
		prompt:          deps.Prompt,
		hooks:           deps.Hooks,
		compaction:      deps.Compaction,
		workspace:       deps.Workspace,
	}, nil
}

// SessionStore returns the durable session backend used for turn history.
func (a *Runtime) SessionStore() agentkit.SessionStore { return a.sessionStore }

func (a *Runtime) ID() agentkit.AgentID { return a.id }

// ConfiguredModel returns the model from agent config (before session override).
func (a *Runtime) ConfiguredModel() string { return a.model }

func (a *Runtime) effectiveModel(ctx context.Context, sess agentkit.Session) string {
	if sess == nil {
		return a.model
	}
	effective, _, _, err := sessbind.ResolveEffectiveModel(ctx, a.sessionStore, a.workspace, sess.ID(), a.id, a.model)
	if err != nil || effective == "" {
		return a.model
	}
	return effective
}

// turnRun holds mutable state for one turn, spanning every segment the
// TurnStopping hooks extend it with.
type turnRun struct {
	meter     *turnMeter
	completed int
	llmModel  string
}

func (a *Runtime) RunTurn(ctx context.Context, input agentkit.TurnInput) (runErr error) {
	sessionID := rctx.SessionIDFromContext(ctx)
	if sessionID == "" {
		return fmt.Errorf("turn requires session id in context")
	}
	ctx = rctx.WithWorkspaceService(ctx, a.workspace)
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

	run := &turnRun{
		meter:    newTurnMeter(),
		llmModel: a.effectiveModel(ctx, sess),
	}
	if err := a.emitLifecycle(ctx, input.Emit, agentkit.EventTurnStart, capsession.TurnStartData{}); err != nil {
		return err
	}
	if err := sessevents.Default.AppendTurnStart(ctx, sess, a.id); err != nil {
		return err
	}
	defer func() {
		endCtx := context.WithoutCancel(ctx)
		telemetry.RecordTurnSteps(ctx, run.completed)
		endData := capsession.TurnEndData{Steps: run.completed}
		cancelled := false
		if cap, ok := stepLimitFromError(runErr); ok {
			endData.StopReason = string(agentkit.StopStepLimit)
			endData.StepLimit = cap
			telemetry.RecordTurnStopReason(ctx, endData.StopReason)
		} else if reason, ok := cancelReasonFromError(runErr); ok {
			cancelled = true
			endData.Cancelled = true
			endData.StopReason = reason
			telemetry.RecordTurnStopReason(ctx, reason)
		} else if runErr != nil {
			endData.Failed = true
			endData.StopReason = runErr.Error()
		}
		_ = sessevents.Default.AppendTurnEnd(endCtx, sess, a.id, endData)
		if err := a.emitLifecycle(endCtx, input.Emit, agentkit.EventTurnEnd, endData); err != nil {
			slog.Debug("agent: emit turn/end failed", "agent_id", a.id, "session_id", sessionID, "err", err)
		}
		if a.hooks != nil && runErr == nil && !cancelled {
			a.invokeTurnComplete(endCtx, sessionID, sess, run.llmModel, run.meter)
		}
	}()

	if err := sessevents.Default.AppendMessage(ctx, sess, a.id, agentkit.EventUserMessage, input.Message); err != nil {
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
			// Steering may arrive after the last step returned but before this turn
			// unwinds (e.g. outbound delivery). Keep the turn alive for it.
			if ctrl.HasSteering() {
				continue
			}
			stopReason := string(reason)
			telemetry.RecordTurnStopReason(ctx, stopReason)
			telemetry.RecordEvent(ctx, "turn.completed", map[string]string{
				"steps":       fmt.Sprint(run.completed),
				"stop_reason": stopReason,
			})
			return nil
		}
	}
}

// runSegment drives model steps until the assistant stops requesting tools.
func (a *Runtime) runSegment(
	ctx context.Context,
	sess agentkit.Session,
	emit agentkit.OutboundEmit,
	ctrl turnControl,
	run *turnRun,
) (agentkit.TurnStopReason, error) {
	// Overflow recovery is per segment: a long autonomous run must be able to
	// compact again after the first recovery.
	overflowRecoveryAttempted := false

	for {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		if reason := ctrl.PopCancelReason(); reason != "" {
			return "", fmt.Errorf("cancelled: %s", reason)
		}

		for _, msg := range ctrl.PopSteering() {
			if err := sessevents.Default.AppendMessage(ctx, sess, a.id, agentkit.EventUserMessage, msg); err != nil {
				return "", err
			}
		}

		if a.maxSteps > 0 && run.meter.stepsUsed() >= a.maxSteps {
			slog.Info("step limit reached",
				"agent_id", a.id,
				"session_id", sess.ID(),
				"max_steps", a.maxSteps,
				"steps", run.meter.stepsUsed(),
			)
			return "", newStepLimitError(a.maxSteps)
		}

		run.meter.recordStep()
		stepIndex := run.meter.stepsUsed() - 1
		pos := stepPosition{step: stepIndex, segment: run.meter.continuationsUsed()}

		stepCtx, endStep := ctrl.BeginStep(ctx)
		stepDone := false
		endStepOnce := func() {
			if stepDone {
				return
			}
			stepDone = true
			endStep()
		}

		if err := sessevents.Default.AppendStepStart(ctx, sess, a.id, stepIndex); err != nil {
			endStepOnce()
			return "", err
		}

		stepRetry := newStepRetry(a.retry)

		outcome, err := a.runStepWithOverflowRecovery(stepCtx, sess, emit, run.llmModel, stepRetry, &overflowRecoveryAttempted, pos)
		if err != nil {
			_ = sessevents.Default.AppendStepEnd(context.WithoutCancel(ctx), sess, a.id, stepIndex)
			endStepOnce()
			return "", err
		}

		if err := a.recordUsage(ctx, sess, run, outcome.usage); err != nil {
			endStepOnce()
			return "", err
		}

		assistant := outcome.message
		toolBaseCtx := outcome.ctx
		if toolBaseCtx == nil {
			toolBaseCtx = stepCtx
		}
		for _, call := range assistant.ToolCalls {
			if reason := ctrl.PopCancelReason(); reason != "" {
				_ = sessevents.Default.AppendStepEnd(context.WithoutCancel(ctx), sess, a.id, stepIndex)
				endStepOnce()
				return "", fmt.Errorf("cancelled: %s", reason)
			}
			if err := sessevents.Default.AppendToolCall(ctx, sess, a.id, call); err != nil {
				_ = sessevents.Default.AppendStepEnd(context.WithoutCancel(ctx), sess, a.id, stepIndex)
				endStepOnce()
				return "", err
			}
			toolCtx := withToolContext(toolBaseCtx, sess, a.id)
			result, err := a.tools.Execute(toolCtx, call)
			if err != nil {
				_ = sessevents.Default.AppendStepEnd(context.WithoutCancel(ctx), sess, a.id, stepIndex)
				endStepOnce()
				return "", err
			}
			stored, err := derive.PrepareToolResultForStorage(ctx, sess.ID(), result, 0)
			if err != nil {
				_ = sessevents.Default.AppendStepEnd(context.WithoutCancel(ctx), sess, a.id, stepIndex)
				endStepOnce()
				return "", err
			}
			if err := sessevents.Default.AppendToolResult(ctx, sess, a.id, stored); err != nil {
				_ = sessevents.Default.AppendStepEnd(context.WithoutCancel(ctx), sess, a.id, stepIndex)
				endStepOnce()
				return "", err
			}
			if err := a.emitLifecycle(ctx, emit, agentkit.EventToolResult, stored); err != nil {
				_ = sessevents.Default.AppendStepEnd(context.WithoutCancel(ctx), sess, a.id, stepIndex)
				endStepOnce()
				return "", err
			}
		}

		if err := sessevents.Default.AppendStepEnd(ctx, sess, a.id, stepIndex); err != nil {
			endStepOnce()
			return "", err
		}
		run.completed++
		endStepOnce()

		if len(assistant.ToolCalls) == 0 {
			return agentkit.StopNoToolCalls, nil
		}
	}
}

// extendTurn consults TurnStopping hooks. When they ask to keep going, the
// continuation is recorded as a turn/continue event, which derive replays as a
// user message for the next segment.
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
	messages, err := sess.DeriveMessages(ctx)
	if err != nil {
		return false, err
	}
	stopping := &agentkit.TurnStopping{
		Reason:   reason,
		Steps:    run.meter.stepsUsed(),
		Segments: run.meter.continuationsUsed(),
		Tokens:   run.meter.tokensUsed(),
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
				"steps", run.meter.stepsUsed(),
				"continuations", run.meter.continuationsUsed(),
			)
		}
		return false, nil
	}

	run.meter.recordContinuation()
	data := capsession.TurnContinueData{
		Segment:  run.meter.continuationsUsed(),
		Reason:   string(reason),
		Steps:    run.meter.stepsUsed(),
		Messages: stopping.Continue,
	}
	if err := sessevents.Default.AppendTurnContinue(ctx, sess, a.id, data); err != nil {
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
			AgentID: a.id,
			Type:    agentkit.EventTurnContinue,
			Data:    rctx.MarshalOutboundData(data),
		}); err != nil {
			return false, err
		}
	}
	return true, nil
}

// recordUsage logs token accounting for one step.
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
	run.meter.recordTokens(total)
	telemetry.RecordTurnUsage(ctx, captelemetry.Usage{
		InputTokens:  usage.InputTokens,
		OutputTokens: usage.OutputTokens,
		TotalTokens:  total,
	})
	return sessevents.Default.AppendUsage(ctx, sess, a.id, capsession.UsageData{
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
	// ctx carries telemetry parent ids for tool calls spawned by this step.
	ctx context.Context
}

func (a *Runtime) runStep(ctx context.Context, sess agentkit.Session, emit agentkit.OutboundEmit, model string, pos stepPosition) (stepOutcome, error) {
	stepStarted := time.Now()
	ctx, endPrep := telemetry.BeginObservation(ctx, telemetry.ObservationMetaFromContext(ctx, captelemetry.ObservationMeta{
		Name: "agent.step.prep",
		Kind: captelemetry.KindSpan,
	}))
	var prepEnd captelemetry.ObservationEnd
	prepDone := false
	finishPrep := func() {
		if prepDone {
			return
		}
		prepDone = true
		endPrep(prepEnd)
	}

	history, ctx, err := a.prepareStepHistory(ctx, sess, pos)
	if err != nil {
		prepEnd.Err = err
		finishPrep()
		return stepOutcome{}, err
	}
	specs, err := a.tools.Visible(ctx)
	if err != nil {
		prepEnd.Err = err
		finishPrep()
		return stepOutcome{}, err
	}
	messages, err := a.prompt.Assemble(ctx, agentkit.PromptRequest{
		Messages: history,
	})
	if err != nil {
		prepEnd.Err = err
		finishPrep()
		return stepOutcome{}, err
	}
	if a.maxPromptTokens > 0 && len(a.compaction) > 0 {
		messages, ctx, err = a.guardPromptSize(ctx, sess, pos, messages)
		if err != nil {
			prepEnd.Err = err
			finishPrep()
			return stepOutcome{}, err
		}
	}
	prepEnd.Output = fmt.Sprintf("%d messages, %d tools", len(messages), len(specs))
	finishPrep()

	ctx, endObservation := telemetry.BeginObservation(ctx, telemetry.ObservationMetaFromContext(ctx, captelemetry.ObservationMeta{
		Name:               "llm.generation",
		Kind:               captelemetry.KindGeneration,
		Model:              model,
		Input:              telemetry.ExportMessages(messages),
		GenerationMessages: messages,
		ToolNames:          telemetry.ToolNamesFromSpecs(specs),
	}))
	var observationEnd captelemetry.ObservationEnd
	defer func() {
		endObservation(observationEnd)
	}()

	stream, err := a.llm.Stream(ctx, agentkit.LLMRequest{
		Model:    model,
		Messages: messages,
		Tools:    specs,
	})
	if err != nil {
		observationEnd.Err = err
		return stepOutcome{}, err
	}
	defer stream.Close()

	streamOut := newStreamEmitter(ctx, sess.ID(), a.id, emit)

	var assistant agentkit.ModelMessage
	var usage *agentkit.Usage
	gotFirstCompletion := false
	gotFirstText := false
	for {
		if err := ctx.Err(); err != nil {
			observationEnd.Err = err
			_ = persistAssistantWithStopReason(ctx, sess, a.id, assistant, agentkit.AssistantStopReasonAborted)
			return stepOutcome{}, err
		}
		ev, err := stream.Recv()
		if !gotFirstCompletion && telemetry.LLMCompletionStarted(ev) {
			observationEnd.CompletionStartTime = time.Now().UTC()
			gotFirstCompletion = true
		}
		if !gotFirstText && telemetry.LLMTextStarted(ev) {
			observationEnd.FirstTextTime = time.Now().UTC()
			gotFirstText = true
		}
		if ev.Message != nil {
			assistant = *ev.Message
		}
		if ev.Usage != nil {
			usage = ev.Usage
		}
		if consumeErr := streamOut.consume(ev); consumeErr != nil {
			observationEnd.Err = consumeErr
			return stepOutcome{}, consumeErr
		}
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			observationEnd.Err = err
			_ = persistAssistantWithStopReason(ctx, sess, a.id, assistant, agentkit.AssistantStopReasonError)
			return stepOutcome{}, err
		}
	}
	if assistant.Role == "" {
		assistant.Role = "assistant"
	}
	if err := streamOut.finalize(assistant); err != nil {
		observationEnd.Err = err
		return stepOutcome{}, err
	}

	if err := appendAssistantMessage(ctx, sess, a.id, assistant); err != nil {
		observationEnd.Err = err
		return stepOutcome{}, err
	}
	toolNames := make([]string, len(assistant.ToolCalls))
	for i, call := range assistant.ToolCalls {
		toolNames[i] = call.Name
	}
	attrs := []any{
		"agent_id", a.id,
		"session_id", sess.ID(),
		"model", model,
		"tool_calls", len(assistant.ToolCalls),
		"duration", time.Since(stepStarted),
	}
	if len(toolNames) > 0 {
		attrs = append(attrs, "tools", toolNames)
	}
	if usage != nil {
		attrs = append(attrs,
			"input_tokens", usage.InputTokens,
			"output_tokens", usage.OutputTokens,
			"total_tokens", usage.TotalTokens,
		)
	}
	slog.Info("assistant step", attrs...)

	var usageOut *captelemetry.Usage
	if usage != nil {
		total := usage.TotalTokens
		if total == 0 {
			total = usage.InputTokens + usage.OutputTokens
		}
		usageOut = &captelemetry.Usage{
			InputTokens:  usage.InputTokens,
			OutputTokens: usage.OutputTokens,
			TotalTokens:  total,
		}
	}
	observationEnd.Output = telemetry.FormatMessage(assistant)
	observationEnd.Usage = usageOut

	return stepOutcome{message: assistant, usage: usage, ctx: ctx}, nil
}

// guardPromptSize is the last line of defense before the LLM call: it estimates
// the assembled prompt at real send size (system prompt and inline media
// included), force-runs compaction once when over maxPromptTokens, and fails
// the step when the prompt still does not fit.
func (a *Runtime) guardPromptSize(ctx context.Context, sess agentkit.Session, pos stepPosition, messages []agentkit.ModelMessage) ([]agentkit.ModelMessage, context.Context, error) {
	est := rtcompaction.EstimateMessagesTokens(messages)
	if est <= a.maxPromptTokens {
		return messages, ctx, nil
	}
	slog.Warn("prompt exceeds maxPromptTokens, compacting before send",
		"agent_id", a.id,
		"session_id", sess.ID(),
		"estimated_tokens", est,
		"max_prompt_tokens", a.maxPromptTokens,
	)
	applied, compactErr := a.runForcedCompaction(ctx, sess)
	if compactErr != nil {
		return nil, ctx, fmt.Errorf("pre-send compaction failed: %w", compactErr)
	}
	if applied == 0 {
		return nil, ctx, fmt.Errorf("prompt %d tokens exceeds maxPromptTokens %d and compaction did not apply", est, a.maxPromptTokens)
	}
	history, ctx, err := a.prepareStepHistory(ctx, sess, pos)
	if err != nil {
		return nil, ctx, err
	}
	messages, err = a.prompt.Assemble(ctx, agentkit.PromptRequest{Messages: history})
	if err != nil {
		return nil, ctx, err
	}
	if est := rtcompaction.EstimateMessagesTokens(messages); est > a.maxPromptTokens {
		return nil, ctx, fmt.Errorf("prompt %d tokens still exceeds maxPromptTokens %d after compaction", est, a.maxPromptTokens)
	}
	return messages, ctx, nil
}

func (a *Runtime) prepareStepHistory(ctx context.Context, sess agentkit.Session, pos stepPosition) ([]agentkit.ModelMessage, context.Context, error) {
	history, err := sess.DeriveMessages(ctx)
	if err != nil {
		return nil, ctx, err
	}
	mods := a.modalities
	if len(mods) == 0 {
		mods = rtllm.ProviderModalities(a.llm)
	}
	history, err = derive.PrepareMessagesForLLM(ctx, history, a.workspace, 0, mods)
	if err != nil {
		return nil, ctx, err
	}
	if a.hooks == nil {
		return history, ctx, nil
	}
	step := &agentkit.BeforeStep{Step: pos.step, Segment: pos.segment, Messages: history}
	if err := a.hooks.BeforeStep(ctx, step); err != nil {
		return nil, ctx, err
	}
	if step.Messages != nil {
		return step.Messages, ctx, nil
	}
	return history, ctx, nil
}

func (a *Runtime) invokeTurnComplete(ctx context.Context, sessionID agentkit.SessionID, sess agentkit.Session, model string, meter *turnMeter) {
	if sess == nil || sess.ID() != sessionID {
		loaded, err := sessevents.LoadSession(ctx, a.sessionStore, sessionID)
		if err != nil {
			slog.Debug("agent: turn complete skipped, session reload failed",
				"agent_id", a.id, "session_id", sessionID, "err", err)
			return
		}
		sess = loaded
	}
	messages, err := sess.DeriveMessages(ctx)
	if err != nil {
		slog.Debug("agent: turn complete skipped, derive messages failed",
			"agent_id", a.id, "session_id", sessionID, "err", err)
		return
	}
	tc := &agentkit.TurnComplete{
		AgentID:    a.id,
		SessionID:  sessionID,
		Model:      model,
		Steps:      meter.stepsUsed(),
		Segments:   meter.continuationsUsed(),
		TurnTokens: meter.tokensUsed(),
		Messages:   messages,
	}
	if err := a.hooks.TurnComplete(ctx, tc); err != nil {
		slog.Warn("agent: turn complete hook failed",
			"agent_id", a.id, "session_id", sessionID, "err", err)
	}
}

func withToolContext(ctx context.Context, sess agentkit.Session, agentID agentkit.AgentID) context.Context {
	env := rctx.EnvelopeFromContext(ctx)
	if sess != nil && sess.ID() != "" {
		env = env.WithConversation(string(sess.ID()))
	}
	ctx = rctx.ApplyEnvelopeToContext(ctx, env)
	if agentID != "" {
		ctx = rctx.WithAgentID(ctx, agentID)
	}
	ctx = rctx.WithSession(ctx, sess)
	return ctx
}

func EncodeEventData(v any) json.RawMessage {
	raw, _ := json.Marshal(v)
	return raw
}

func appendAssistantMessage(ctx context.Context, sess agentkit.Session, agentID agentkit.AgentID, msg agentkit.ModelMessage) error {
	if msg.Role == "" {
		msg.Role = "assistant"
	}
	return sessevents.Default.AppendMessage(ctx, sess, agentID, agentkit.EventAssistantMessage, msg)
}

func assistantMessageWorthPersisting(msg agentkit.ModelMessage) bool {
	if len(msg.ToolCalls) > 0 {
		return true
	}
	for _, part := range msg.Content {
		if strings.TrimSpace(part.Text) != "" {
			return true
		}
	}
	return false
}

func persistAssistantWithStopReason(ctx context.Context, sess agentkit.Session, agentID agentkit.AgentID, msg agentkit.ModelMessage, stopReason string) error {
	if !assistantMessageWorthPersisting(msg) {
		return nil
	}
	msg.StopReason = stopReason
	return appendAssistantMessage(context.WithoutCancel(ctx), sess, agentID, msg)
}

func cancelReasonFromError(err error) (string, bool) {
	if err == nil {
		return "", false
	}
	const prefix = "cancelled: "
	msg := err.Error()
	if strings.HasPrefix(msg, prefix) {
		reason := strings.TrimSpace(msg[len(prefix):])
		if reason == "" {
			reason = "cancelled"
		}
		return reason, true
	}
	if errors.Is(err, context.Canceled) {
		return "cancelled", true
	}
	return "", false
}

func (a *Runtime) emitLifecycle(ctx context.Context, emit agentkit.OutboundEmit, typ agentkit.EventType, data any) error {
	if emit == nil {
		return nil
	}
	return emit(ctx, agentkit.OutboundEvent{
		AgentID: a.id,
		Type:    typ,
		Data:    rctx.MarshalOutboundData(data),
	})
}
