package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"

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

	mu            sync.Mutex
	steering      []agentkit.ModelMessage
	followUps     []agentkit.ModelMessage
	cancelReason  string
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
		id:       id,
		model:    cfg.Model,
		maxSteps: maxSteps,
		llm:      deps.LLM,
		session:  deps.Session,
		tools:    deps.Tools,
		prompt:   deps.Prompt,
		hooks:    deps.Hooks,
	}, nil
}

func (a *Runtime) ID() agentkit.AgentID              { return a.id }
func (a *Runtime) Session() agentkit.Session         { return a.session }

func (a *Runtime) Steer(_ context.Context, msg agentkit.ModelMessage) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.steering = append(a.steering, msg)
	return nil
}

func (a *Runtime) FollowUp(_ context.Context, msg agentkit.ModelMessage) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.followUps = append(a.followUps, msg)
	return nil
}

func (a *Runtime) Cancel(_ context.Context, reason string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.cancelReason = reason
	return nil
}

func (a *Runtime) WhenIdle(context.Context) error { return nil }

func (a *Runtime) RunTurn(ctx context.Context, input agentkit.TurnInput) (agentkit.TurnResult, error) {
	a.mu.Lock()
	a.cancelReason = ""
	a.mu.Unlock()

	if err := session.AppendTurnStart(ctx, a.session, a.id); err != nil {
		return agentkit.TurnResult{}, err
	}
	stepsCompleted := 0
	defer func() {
		_ = session.AppendTurnEnd(context.WithoutCancel(ctx), a.session, a.id, stepsCompleted)
	}()

	if err := session.AppendMessage(ctx, a.session, a.id, agentkit.EventUserMessage, input.Message); err != nil {
		return agentkit.TurnResult{}, err
	}

	var collected []agentkit.ModelMessage
	collected = append(collected, input.Message)

	for step := 0; step < a.maxSteps; step++ {
		if err := ctx.Err(); err != nil {
			return agentkit.TurnResult{}, err
		}
		if reason := a.popCancelReason(); reason != "" {
			return agentkit.TurnResult{Messages: collected}, fmt.Errorf("cancelled: %s", reason)
		}

		for _, msg := range a.popSteering() {
			if err := session.AppendMessage(ctx, a.session, a.id, agentkit.EventUserMessage, msg); err != nil {
				return agentkit.TurnResult{}, err
			}
			collected = append(collected, msg)
		}

		if err := session.AppendStepStart(ctx, a.session, a.id, step); err != nil {
			return agentkit.TurnResult{}, err
		}

		assistant, err := a.runStep(ctx, input.Emit)
		if err != nil {
			_ = session.AppendStepEnd(context.WithoutCancel(ctx), a.session, a.id, step)
			return agentkit.TurnResult{}, err
		}
		collected = append(collected, assistant)

		for _, call := range assistant.ToolCalls {
			if err := session.AppendToolCall(ctx, a.session, a.id, call); err != nil {
				_ = session.AppendStepEnd(context.WithoutCancel(ctx), a.session, a.id, step)
				return agentkit.TurnResult{}, err
			}
			scope := agentkit.ToolScope{
				SessionID: a.session.ID(),
				AgentID:   a.id,
				Session:   a.session,
			}
			toolCtx := tools.WithScope(ctx, scope)
			result, err := a.tools.Execute(toolCtx, call)
			if err != nil {
				_ = session.AppendStepEnd(context.WithoutCancel(ctx), a.session, a.id, step)
				return agentkit.TurnResult{}, err
			}
			if err := session.AppendToolResult(ctx, a.session, a.id, result); err != nil {
				_ = session.AppendStepEnd(context.WithoutCancel(ctx), a.session, a.id, step)
				return agentkit.TurnResult{}, err
			}
			collected = append(collected, toolResultMessage(result))
		}

		if err := session.AppendStepEnd(ctx, a.session, a.id, step); err != nil {
			return agentkit.TurnResult{}, err
		}
		stepsCompleted++

		if len(assistant.ToolCalls) == 0 {
			break
		}
	}

	return agentkit.TurnResult{Messages: collected}, nil
}

func (a *Runtime) runStep(ctx context.Context, emit agentkit.OutboundEmit) (agentkit.ModelMessage, error) {
	history, err := a.prepareStepHistory(ctx)
	if err != nil {
		return agentkit.ModelMessage{}, err
	}
	specs, err := a.tools.Visible(ctx, agentkit.ToolScope{SessionID: a.session.ID(), AgentID: a.id})
	if err != nil {
		return agentkit.ModelMessage{}, err
	}
	prompt, err := a.prompt.Assemble(ctx, agentkit.PromptRequest{
		SessionID: a.session.ID(),
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

	streamOut := newStreamEmitter(ctx, a.session.ID(), a.id, emit)

	var assistant agentkit.ModelMessage
	for {
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

	if err := session.AppendMessage(ctx, a.session, a.id, agentkit.EventAssistantMessage, assistant); err != nil {
		return agentkit.ModelMessage{}, err
	}
	slog.Info("assistant step", "agent_id", a.id, "tool_calls", len(assistant.ToolCalls))
	return assistant, nil
}

func (a *Runtime) prepareStepHistory(ctx context.Context) ([]agentkit.ModelMessage, error) {
	history, err := a.session.DeriveMessages(ctx)
	if err != nil {
		return nil, err
	}
	if a.hooks == nil {
		return history, nil
	}
	step := &agentkit.BeforeStep{
		SessionID: a.session.ID(),
		AgentID:   a.id,
		Session:   a.session,
		Messages:  history,
	}
	if err := a.hooks.BeforeStep(ctx, step); err != nil {
		return nil, err
	}
	if step.Messages != nil {
		return step.Messages, nil
	}
	return a.session.DeriveMessages(ctx)
}

func (a *Runtime) popSteering() []agentkit.ModelMessage {
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.steering) == 0 {
		return nil
	}
	out := append([]agentkit.ModelMessage(nil), a.steering...)
	a.steering = nil
	return out
}

func (a *Runtime) popCancelReason() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	reason := a.cancelReason
	a.cancelReason = ""
	return reason
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
