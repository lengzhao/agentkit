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
	"github.com/lengzhao/agentkit/runtime/tools"
)

type Config struct {
	ID       agentkit.AgentID `json:"id"`
	Model    string           `json:"model"`
	MaxSteps int              `json:"maxSteps"`
}

type Deps struct {
	SessionStore agentkit.SessionStore `json:"sessionStore"`
	LLM          agentkit.LLMProvider  `json:"llm"`
	Tools        agentkit.ToolRuntime  `json:"tools"`
	Prompt       agentkit.PromptAssembler `json:"prompt"`
	Policies     []agentkit.Policy     `json:"policies,omitempty"`
	Hooks        agentkit.HookRuntime  `json:"hooks,omitempty"`
}

type Runtime struct {
	id           agentkit.AgentID
	model        string
	maxSteps     int
	sessionStore agentkit.SessionStore
	llm          agentkit.LLMProvider
	tools        agentkit.ToolRuntime
	prompt       agentkit.PromptAssembler
	hooks        agentkit.HookRuntime
}

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
		id:             id,
		model:          cfg.Model,
		maxSteps:       maxSteps,
		sessionStore:   deps.SessionStore,
		llm:            deps.LLM,
		tools:          deps.Tools,
		prompt:       deps.Prompt,
		hooks:        deps.Hooks,
	}, nil
}

func (a *Runtime) ID() agentkit.AgentID { return a.id }

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

	if err := session.AppendTurnStart(ctx, sess, a.id); err != nil {
		return err
	}
	stepsCompleted := 0
	defer func() {
		_ = session.AppendTurnEnd(context.WithoutCancel(ctx), sess, a.id, stepsCompleted)
	}()

	if err := session.AppendMessage(ctx, sess, a.id, agentkit.EventUserMessage, input.Message); err != nil {
		return err
	}

	for step := 0; step < a.maxSteps; step++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		if reason := ctrl.PopCancelReason(); reason != "" {
			return fmt.Errorf("cancelled: %s", reason)
		}

		for _, msg := range ctrl.PopSteering() {
			if err := session.AppendMessage(ctx, sess, a.id, agentkit.EventUserMessage, msg); err != nil {
				return err
			}
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
			return err
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
			return err
		}

		toolInterrupted := false
		for _, call := range assistant.ToolCalls {
			if err := session.AppendToolCall(ctx, sess, a.id, call); err != nil {
				_ = session.AppendStepEnd(context.WithoutCancel(ctx), sess, a.id, step)
				endStepOnce()
				return err
			}
			toolCtx := withToolContext(stepCtx, sessionID, a.id)
			result, err := a.tools.Execute(toolCtx, call)
			if err != nil {
				if ctrl.ShouldContinueAfterInterrupt(ctx, stepCtx, err) {
					toolInterrupted = true
					break
				}
				_ = session.AppendStepEnd(context.WithoutCancel(ctx), sess, a.id, step)
				endStepOnce()
				return err
			}
			if err := session.AppendToolResult(ctx, sess, a.id, result); err != nil {
				_ = session.AppendStepEnd(context.WithoutCancel(ctx), sess, a.id, step)
				endStepOnce()
				return err
			}
		}

		if err := session.AppendStepEnd(ctx, sess, a.id, step); err != nil {
			endStepOnce()
			return err
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

	return nil
}

func (a *Runtime) runStep(ctx context.Context, sess agentkit.Session, emit agentkit.OutboundEmit) (agentkit.ModelMessage, error) {
	history, err := a.prepareStepHistory(ctx, sess)
	if err != nil {
		return agentkit.ModelMessage{}, err
	}
	specs, err := a.tools.Visible(ctx)
	if err != nil {
		return agentkit.ModelMessage{}, err
	}
	prompt, err := a.prompt.Assemble(ctx, agentkit.PromptRequest{
		Messages: history,
		Tools:    specs,
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
