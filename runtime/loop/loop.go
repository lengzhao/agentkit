package loop

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/lengzhao/agentkit"
)

type Config struct {
	DefaultAgent agentkit.AgentID      `json:"defaultAgent"`
	FollowUpMode agentkit.FollowUpMode `json:"followUpMode"`
}

type Deps struct {
	Agents []agentkit.Agent `json:"agents"`
}

type Default struct {
	agents          map[agentkit.AgentID]agentkit.Agent
	defaultAgent    agentkit.AgentID
	followUpMode    agentkit.FollowUpMode
	sessionLocks    sync.Map // SessionID -> *sync.Mutex
	sessionControls sync.Map // SessionID -> *Control
}

func New(cfg Config, deps Deps) (agentkit.Loop, error) {
	agents := make(map[agentkit.AgentID]agentkit.Agent, len(deps.Agents))
	for _, ag := range deps.Agents {
		if ag == nil {
			continue
		}
		agents[ag.ID()] = ag
	}
	defaultID := cfg.DefaultAgent
	if defaultID == "" && len(deps.Agents) > 0 {
		defaultID = deps.Agents[0].ID()
	}
	mode := cfg.FollowUpMode
	if mode == "" {
		mode = agentkit.FollowUpOneAtATime
	}
	if len(deps.Agents) == 0 {
		return nil, fmt.Errorf("loop requires at least one agent")
	}
	return &Default{
		agents:       agents,
		defaultAgent: defaultID,
		followUpMode: mode,
	}, nil
}

func (l *Default) Dispatch(ctx context.Context, req agentkit.LoopRequest) error {
	ag, agentID, err := l.resolveAgent(req.Event.AgentID)
	if err != nil {
		return err
	}
	sessionID := req.Event.SessionID
	if sessionID == "" {
		return fmt.Errorf("message event requires session id")
	}

	unlock := l.lockSession(sessionID)
	defer unlock()

	control := l.controlFor(sessionID)
	ctx = withTurnContext(ctx, sessionID, agentID, req.Event.PlatformID, req.Event.UserID, control, req.Emit, req.InteractionHandler, req.AsyncInteraction)

	turnInput := agentkit.TurnInput{
		Message: req.Event.Message,
		Emit:    req.Emit,
	}
	if err := ag.RunTurn(ctx, turnInput); err != nil {
		return err
	}

	for {
		followUps, err := control.DrainFollowUps(ctx, l.followUpMode)
		if err != nil {
			return err
		}
		if len(followUps) == 0 {
			break
		}
		for _, msg := range followUps {
			if err := ag.RunTurn(ctx, agentkit.TurnInput{
				Message: msg,
				Emit:    req.Emit,
			}); err != nil {
				return err
			}
		}
		if l.followUpMode == agentkit.FollowUpOneAtATime {
			break
		}
	}

	return nil
}

func (l *Default) Steer(ctx context.Context, msg agentkit.ModelMessage) error {
	sessionID, err := sessionIDFromContext(ctx)
	if err != nil {
		return err
	}
	return l.controlFor(sessionID).Steer(ctx, msg)
}

func (l *Default) FollowUp(ctx context.Context, msg agentkit.ModelMessage) error {
	sessionID, err := sessionIDFromContext(ctx)
	if err != nil {
		return err
	}
	return l.controlFor(sessionID).FollowUp(ctx, msg)
}

func (l *Default) TryDeliverInteraction(event agentkit.MessageEvent) bool {
	if event.SessionID == "" {
		return false
	}
	text := messageText(event.Message)
	if strings.TrimSpace(text) == "" {
		return false
	}
	ctrl := l.controlFor(event.SessionID)
	return ctrl.DeliverInteractionReply(event.SessionID, event.InteractionReplyTo, text)
}

func messageText(msg agentkit.ModelMessage) string {
	var b strings.Builder
	for _, part := range msg.Content {
		if part.Type == "text" {
			b.WriteString(part.Text)
		}
	}
	return b.String()
}

func (l *Default) resolveAgent(agentID agentkit.AgentID) (agentkit.Agent, agentkit.AgentID, error) {
	if agentID == "" {
		agentID = l.defaultAgent
	}
	ag, ok := l.agents[agentID]
	if !ok {
		return nil, "", errAgentNotFound(agentID)
	}
	return ag, agentID, nil
}

func (l *Default) lockSession(id agentkit.SessionID) func() {
	v, _ := l.sessionLocks.LoadOrStore(id, &sync.Mutex{})
	mu := v.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}

func withTurnContext(ctx context.Context, sessionID agentkit.SessionID, agentID agentkit.AgentID, platformID string, userID string, control *Control, emit agentkit.OutboundEmit, handler agentkit.InteractionHandler, asyncInteraction bool) context.Context {
	ctx = context.WithValue(ctx, agentkit.KeySessionID, sessionID)
	ctx = context.WithValue(ctx, agentkit.KeyAgentID, agentID)
	if platformID != "" {
		ctx = context.WithValue(ctx, agentkit.KeyPlatformID, platformID)
	}
	if userID != "" {
		ctx = context.WithValue(ctx, agentkit.KeyUserID, userID)
	}
	if emit != nil {
		ctx = context.WithValue(ctx, agentkit.KeyOutboundEmit, emit)
	}
	if handler != nil {
		ctx = context.WithValue(ctx, agentkit.KeyInteractionHandler, handler)
	}
	if asyncInteraction {
		ctx = context.WithValue(ctx, agentkit.KeyAsyncInteraction, true)
	}
	if control != nil {
		ctx = context.WithValue(ctx, agentkit.KeySessionControl, control)
	}
	return ctx
}

func sessionIDFromContext(ctx context.Context) (agentkit.SessionID, error) {
	id, ok := ctx.Value(agentkit.KeySessionID).(agentkit.SessionID)
	if !ok || id == "" {
		return "", fmt.Errorf("session id required in context")
	}
	return id, nil
}

type agentNotFoundError struct{ id agentkit.AgentID }

func (e agentNotFoundError) Error() string       { return "agent not found: " + string(e.id) }
func errAgentNotFound(id agentkit.AgentID) error { return agentNotFoundError{id} }
