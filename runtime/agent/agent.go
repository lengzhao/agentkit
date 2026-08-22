package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/session"
	"github.com/lengzhao/agentkit/runtime/sessioncontrol"
	"github.com/lengzhao/agentkit/runtime/tools"
)

type Config struct {
	ID       agentkit.AgentID `json:"id"`
	Model    string           `json:"model"`
	MaxSteps int              `json:"maxSteps"`
}

type Deps struct {
	LLM      agentkit.LLMProvider     `json:"llm"`
	Session  agentkit.Session         `json:"session"`
	Tools    agentkit.ToolRuntime     `json:"tools"`
	Prompt   agentkit.PromptAssembler `json:"prompt"`
	Policies []agentkit.Policy        `json:"policies,omitempty"`
	Hooks    agentkit.HookRuntime     `json:"hooks,omitempty"`
}

type Runtime struct {
	id       agentkit.AgentID
	model    string
	maxSteps int
	llm      agentkit.LLMProvider
	session  agentkit.Session
	tools    agentkit.ToolRuntime
	prompt   agentkit.PromptAssembler
	hooks    agentkit.HookRuntime

	defaultControl *sessioncontrol.Control
}

func New(cfg Config, deps Deps) (*Runtime, error) {
	id := cfg.ID
	if id == "" {
		id = "coding"
	}
	maxSteps := cfg.MaxSteps
	if maxSteps <= 0 {
		maxSteps = 20
	}
	if deps.LLM == nil {
		return nil, fmt.Errorf("agent requires llm")
	}
	if deps.Session == nil {
		return nil, fmt.Errorf("agent requires session")
	}
	if deps.Tools == nil {
		return nil, fmt.Errorf("agent requires tools runtime")
	}
	if deps.Prompt == nil {
		return nil, fmt.Errorf("agent requires prompt assembler")
	}
	return &Runtime{
		id:             id,
		model:          cfg.Model,
		maxSteps:       maxSteps,
		llm:            deps.LLM,
		session:        deps.Session,
		tools:          deps.Tools,
		prompt:         deps.Prompt,
		hooks:          deps.Hooks,
		defaultControl: sessioncontrol.New(),
	}, nil
}

func (a *Runtime) ID() agentkit.AgentID             { return a.id }
func (a *Runtime) Session() agentkit.Session        { return a.session }
func (a *Runtime) Control() agentkit.SessionControl { return a.defaultControl }

func (a *Runtime) controlForTurn(input agentkit.TurnInput) sessioncontrol.TurnControl {
	if ctrl, ok := input.Control.(sessioncontrol.TurnControl); ok {
		return ctrl
	}
	return a.defaultControl
}

func (a *Runtime) sessionForTurn(input agentkit.TurnInput) agentkit.Session {
	if input.Session != nil {
		return input.Session
	}
	return a.session
}

func (a *Runtime) RunTurn(ctx context.Context, input agentkit.TurnInput) (agentkit.TurnResult, error) {
	sess := a.sessionForTurn(input)
	ctrl := a.controlForTurn(input)

	ctrl.ClearTurnCancel()

	if err := session.AppendTurnStart(ctx, sess, a.id); err != nil {
		return agentkit.TurnResult{}, err
	}
	stepsCompleted := 0
	defer func() {
		_ = session.AppendTurnEnd(context.WithoutCancel(ctx), sess, a.id, stepsCompleted)
	}()

	if err := session.AppendMessage(ctx, sess, a.id, agentkit.EventUserMessage, input.Message); err != nil {
		return agentkit.TurnResult{}, err
	}

	var collected []agentkit.ModelMessage
	collected = append(collected, input.Message)

	for step := 0; step < a.maxSteps; step++ {
		if err := ctx.Err(); err != nil {
			return agentkit.TurnResult{}, err
		}
		if reason := ctrl.PopCancelReason(); reason != "" {
			return agentkit.TurnResult{Messages: collected}, fmt.Errorf("cancelled: %s", reason)
		}

		for _, msg := range ctrl.PopSteering() {
			if err := session.AppendMessage(ctx, sess, a.id, agentkit.EventUserMessage, msg); err != nil {
				return agentkit.TurnResult{}, err
			}
			collected = append(collected, msg)
		}

		stepCtx, endStep := ctrl.BeginStep(ctx)
		stepDone := false
		endStepOnce := func() {
			if stepDone {
				return
			}
			stepDone = true
			endStep()
		}

		if err := session.AppendStepStart(ctx, sess, a.id, step); err != nil {
			endStepOnce()
			return agentkit.TurnResult{}, err
		}

		assistant, err := a.runStep(stepCtx, sess, input.Emit)
		if err != nil {
			if ctrl.ShouldContinueAfterInterrupt(ctx, stepCtx, err) {
				_ = session.AppendStepEnd(context.WithoutCancel(ctx), sess, a.id, step)
				endStepOnce()
				continue
			}
			_ = session.AppendStepEnd(context.WithoutCancel(ctx), sess, a.id, step)
			endStepOnce()
			return agentkit.TurnResult{}, err
		}
		collected = append(collected, assistant)

		toolInterrupted := false
		for _, call := range assistant.ToolCalls {
			if err := session.AppendToolCall(ctx, sess, a.id, call); err != nil {
				_ = session.AppendStepEnd(context.WithoutCancel(ctx), sess, a.id, step)
				endStepOnce()
				return agentkit.TurnResult{}, err
			}
			scope := agentkit.ToolScope{
				SessionID: sess.ID(),
				AgentID:   a.id,
				Session:   sess,
			}
			toolCtx := tools.WithScope(stepCtx, scope)
			result, err := a.tools.Execute(toolCtx, call)
			if err != nil {
				if ctrl.ShouldContinueAfterInterrupt(ctx, stepCtx, err) {
					toolInterrupted = true
					break
				}
				_ = session.AppendStepEnd(context.WithoutCancel(ctx), sess, a.id, step)
				endStepOnce()
				return agentkit.TurnResult{}, err
			}
			if err := session.AppendToolResult(ctx, sess, a.id, result); err != nil {
				_ = session.AppendStepEnd(context.WithoutCancel(ctx), sess, a.id, step)
				endStepOnce()
				return agentkit.TurnResult{}, err
			}
			collected = append(collected, toolResultMessage(result))
		}

		if err := session.AppendStepEnd(ctx, sess, a.id, step); err != nil {
			endStepOnce()
			return agentkit.TurnResult{}, err
		}
		stepsCompleted++
		endStepOnce()

		if toolInterrupted {
			continue
		}

		if len(assistant.ToolCalls) == 0 {
			break
		}
	}

	return agentkit.TurnResult{Messages: collected}, nil
}

func (a *Runtime) runStep(ctx context.Context, sess agentkit.Session, emit agentkit.OutboundEmit) (agentkit.ModelMessage, error) {
	history, err := a.prepareStepHistory(ctx, sess)
	if err != nil {
		return agentkit.ModelMessage{}, err
	}
	specs, err := a.tools.Visible(ctx, agentkit.ToolScope{SessionID: sess.ID(), AgentID: a.id})
	if err != nil {
		return agentkit.ModelMessage{}, err
	}
	prompt, err := a.prompt.Assemble(ctx, agentkit.PromptRequest{
		SessionID: sess.ID(),
		AgentID:   a.id,
		Messages:  history,
		Tools:     specs,
	})
	if err != nil {
		return agentkit.ModelMessage{}, err
	}

	stream, err := a.llm.Stream(ctx, agentkit.LLMRequest{
		Model:    a.model,
		Messages: prompt.Messages,
		Tools:    specs,
	})
	if err != nil {
		return agentkit.ModelMessage{}, err
	}
	defer stream.Close()

	streamOut := newStreamEmitter(ctx, sess.ID(), a.id, emit)

	var assistant agentkit.ModelMessage
	for {
		if err := ctx.Err(); err != nil {
			return agentkit.ModelMessage{}, err
		}
		ev, err := stream.Recv()
		if ev.Message != nil {
			assistant = *ev.Message
		}
		if consumeErr := streamOut.consume(ev); consumeErr != nil {
			return agentkit.ModelMessage{}, consumeErr
		}
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return agentkit.ModelMessage{}, err
		}
	}
	if assistant.Role == "" {
		assistant.Role = "assistant"
	}
	if err := streamOut.finalize(assistant); err != nil {
		return agentkit.ModelMessage{}, err
	}

	if err := session.AppendMessage(ctx, sess, a.id, agentkit.EventAssistantMessage, assistant); err != nil {
		return agentkit.ModelMessage{}, err
	}
	slog.Info("assistant step", "agent_id", a.id, "session_id", sess.ID(), "tool_calls", len(assistant.ToolCalls))
	return assistant, nil
}

func (a *Runtime) prepareStepHistory(ctx context.Context, sess agentkit.Session) ([]agentkit.ModelMessage, error) {
	history, err := sess.DeriveMessages(ctx)
	if err != nil {
		return nil, err
	}
	if a.hooks == nil {
		return history, nil
	}
	step := &agentkit.BeforeStep{
		SessionID: sess.ID(),
		AgentID:   a.id,
		Session:   sess,
		Messages:  history,
	}
	if err := a.hooks.BeforeStep(ctx, step); err != nil {
		return nil, err
	}
	if step.Messages != nil {
		return step.Messages, nil
	}
	return sess.DeriveMessages(ctx)
}

func toolResultMessage(result agentkit.ToolResult) agentkit.ModelMessage {
	return agentkit.ModelMessage{
		Role:        "tool",
		ToolResults: []agentkit.ToolResult{result},
		Content:     []agentkit.ContentPart{{Type: "text", Text: tools.ResultText(result)}},
	}
}

func EncodeEventData(v any) json.RawMessage {
	raw, _ := json.Marshal(v)
	return raw
}
