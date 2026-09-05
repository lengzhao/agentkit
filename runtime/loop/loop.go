package loop

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/lengzhao/agentkit"
	capschedule "github.com/lengzhao/agentkit/cap/schedule"
	"github.com/lengzhao/agentkit/cap/permission"
	"github.com/lengzhao/agentkit/cap/telemetry"
	"github.com/lengzhao/agentkit/runtime/session"
)

type Config struct {
	// DefaultAgent is agent id used when the event names none; defaults to the single configured agent.
	DefaultAgent agentkit.AgentID `json:"defaultAgent"`
	// FollowUpMode is how follow-up messages are drained after a turn ends.
	FollowUpMode agentkit.FollowUpMode `json:"followUpMode"`
}

type Deps struct {
	Agents    []agentkit.Agent      `json:"agents"`
	Telemetry telemetry.Exporter    `json:"telemetry,omitempty"`
}

type Default struct {
	agents          map[agentkit.AgentID]agentkit.Agent
	defaultAgent    agentkit.AgentID
	followUpMode    agentkit.FollowUpMode
	telemetry       telemetry.Exporter
	sessionLocks    sync.Map // SessionID -> *sync.Mutex
	sessionControls sync.Map // SessionID -> *Control
	sessionBusy     sync.Map // SessionID -> struct{}
}

// New registers loop/default: Route inbound messages to an agent and serialize turns per session.
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
	exp := deps.Telemetry
	if exp == nil {
		exp = telemetry.Noop
	}
	return &Default{
		agents:       agents,
		defaultAgent: defaultID,
		followUpMode: mode,
		telemetry:    exp,
	}, nil
}

// Agents returns configured agent instances in arbitrary order.
func (l *Default) Agents() []agentkit.Agent {
	out := make([]agentkit.Agent, 0, len(l.agents))
	for _, ag := range l.agents {
		out = append(out, ag)
	}
	return out
}

func (l *Default) Dispatch(ctx context.Context, req agentkit.LoopRequest) error {
	ag, agentID, err := l.resolveAgent(req.Event.AgentID)
	if err != nil {
		return err
	}
	conversation := session.ConversationFromLoopRequest(req)
	if conversation == "" {
		return fmt.Errorf("loop request requires conversation")
	}

	unlock := l.lockSession(conversation)
	defer unlock()

	l.markSessionBusy(conversation, true)
	defer l.markSessionBusy(conversation, false)

	control := l.controlFor(conversation)
	capab := permissionCapability(req.Capability)
	control.setTurnCapability(capab)
	ctx = withTurnContext(ctx, req.Event.Envelope, conversation, agentID, req.Event.PlatformID, req.Event.UserID, req.Event.Metadata, control, req.Emit)
	ctx = telemetry.WithExporter(ctx, l.telemetry)

	turnInput := agentkit.TurnInput{
		Message: req.Event.Message,
		Emit:    req.Emit,
	}
	if err := l.runTurn(ctx, req, agentID, ag, turnInput); err != nil {
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
			if err := l.runTurn(ctx, req, agentID, ag, agentkit.TurnInput{
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

func (l *Default) runTurn(ctx context.Context, req agentkit.LoopRequest, agentID agentkit.AgentID, ag agentkit.Agent, input agentkit.TurnInput) error {
	turnID := uuid.NewString()
	meta := telemetry.TurnMeta{
		TurnID:            turnID,
		SessionID:         string(session.ConversationFromLoopRequest(req)),
		DeliverySessionID: string(session.DeliveryFromEnvelope(req.Event.Envelope)),
		AgentID:           string(agentID),
		PlatformID:        req.Event.PlatformID,
		UserID:            req.Event.UserID,
		Input:             telemetry.FormatMessage(input.Message),
	}
	slog.Info("turn start",
		"turn_id", turnID,
		"agent_id", agentID,
		"session_id", session.ConversationFromLoopRequest(req),
		"delivery_session_id", session.DeliveryFromEnvelope(req.Event.Envelope),
		"platform_id", req.Event.PlatformID,
		"user_id", req.Event.UserID,
	)
	turnStarted := time.Now()
	ctx, endTurn := telemetry.BeginTurn(ctx, meta)
	ctx = telemetry.WithTurnAccum(ctx)
	var runErr error
	defer func() {
		end := telemetry.TurnEndFromAccum(ctx)
		end.Err = runErr
		endTurn(end)
		attrs := []any{
			"turn_id", turnID,
			"agent_id", agentID,
			"session_id", session.ConversationFromLoopRequest(req),
			"duration", time.Since(turnStarted),
		}
		if end.Steps > 0 {
			attrs = append(attrs, "steps", end.Steps)
		}
		if end.StopReason != "" {
			attrs = append(attrs, "stop_reason", end.StopReason)
		}
		if end.Usage != nil {
			attrs = append(attrs,
				"input_tokens", end.Usage.InputTokens,
				"output_tokens", end.Usage.OutputTokens,
				"total_tokens", end.Usage.TotalTokens,
			)
		}
		if runErr != nil {
			slog.Error("turn end", append(attrs, "err", runErr)...)
			return
		}
		slog.Info("turn end", attrs...)
	}()
	ctx = context.WithValue(ctx, agentkit.KeyTurnID, turnID)
	turnInput := input
	turnInput.Emit = telemetry.WrapOutboundEmit(ctx, input.Emit)
	runErr = ag.RunTurn(ctx, turnInput)
	return runErr
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

func (l *Default) Cancel(ctx context.Context, reason string) error {
	sessionID, err := sessionIDFromContext(ctx)
	if err != nil {
		return err
	}
	return l.controlFor(sessionID).Cancel(ctx, reason)
}

// IsSessionBusy reports whether a turn is currently executing for the session.
func (l *Default) IsSessionBusy(sessionID agentkit.SessionID) bool {
	_, ok := l.sessionBusy.Load(sessionID)
	return ok
}

func (l *Default) markSessionBusy(sessionID agentkit.SessionID, busy bool) {
	if busy {
		l.sessionBusy.Store(sessionID, struct{}{})
	} else {
		l.sessionBusy.Delete(sessionID)
	}
}

func (l *Default) controlFor(sessionID agentkit.SessionID) *Control {
	v, _ := l.sessionControls.LoadOrStore(sessionID, NewControl())
	return v.(*Control)
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

func withTurnContext(ctx context.Context, env agentkit.TurnEnvelope, sessionID agentkit.SessionID, agentID agentkit.AgentID, platformID string, userID string, metadata map[string]any, control *Control, emit agentkit.OutboundEmit) context.Context {
	if sessionID != "" {
		env = env.WithConversation(string(sessionID))
	}
	if env.Route.Platform == "" && platformID != "" {
		env.Route.Platform = platformID
	}
	if env.Actor.UserID == "" && userID != "" {
		env.Actor.UserID = userID
	}
	if agentID != "" {
		env.AgentID = agentID
	}
	if len(metadata) > 0 && len(env.Metadata) == 0 {
		env.Metadata = metadata
	}
	ctx = session.ApplyEnvelopeToContext(ctx, env)
	if len(metadata) > 0 {
		if capschedule.IsFireTurn(metadata) {
			ctx = context.WithValue(ctx, agentkit.KeyScheduleFireTurn, true)
		}
		if capschedule.IsFireStateless(metadata) {
			ctx = context.WithValue(ctx, agentkit.KeyScheduleStateless, true)
		}
	}
	if emit != nil {
		ctx = agentkit.ContextWithOutboundEmit(ctx, emit)
	}
	if control != nil {
		ctx = context.WithValue(ctx, agentkit.KeySessionControl, control)
	}
	return ctx
}

func permissionCapability(raw any) permission.Capability {
	capab, ok := raw.(permission.Capability)
	if !ok {
		return permission.Capability{}
	}
	return capab
}

func sessionIDFromContext(ctx context.Context) (agentkit.SessionID, error) {
	id := session.SessionIDFromContext(ctx)
	if id == "" {
		return "", fmt.Errorf("session id required in context")
	}
	return id, nil
}

type agentNotFoundError struct{ id agentkit.AgentID }

func (e agentNotFoundError) Error() string       { return "agent not found: " + string(e.id) }
func errAgentNotFound(id agentkit.AgentID) error { return agentNotFoundError{id} }
