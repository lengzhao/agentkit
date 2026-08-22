package loop

import (
	"context"
	"sync"

	"github.com/lengzhao/agentkit"
)

type Config struct {
	DefaultAgent agentkit.AgentID   `json:"defaultAgent"`
	FollowUpMode agentkit.FollowUpMode `json:"followUpMode"`
}

type Deps struct {
	Agents       []agentkit.Agent      `json:"agents"`
	SessionStore agentkit.SessionStore `json:"sessionStore,omitempty"`
}

type Default struct {
	agents          map[agentkit.AgentID]agentkit.Agent
	defaultAgent    agentkit.AgentID
	followUpMode    agentkit.FollowUpMode
	sessionStore    agentkit.SessionStore
	sessionLocks    sync.Map // SessionID -> *sync.Mutex
	sessionControls sync.Map // SessionID -> *sessioncontrol.Control
}

func New(cfg Config, deps Deps) (*Default, error) {
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
	return &Default{
		agents:       agents,
		defaultAgent: defaultID,
		followUpMode: mode,
		sessionStore: deps.SessionStore,
	}, nil
}

func (l *Default) Dispatch(ctx context.Context, req agentkit.LoopRequest) (agentkit.LoopResult, error) {
	ag, agentID, err := l.resolveAgent(req.Event.AgentID)
	if err != nil {
		return agentkit.LoopResult{}, err
	}

	sess, sessionID, err := l.resolveSession(ctx, req.Event, ag)
	if err != nil {
		return agentkit.LoopResult{}, err
	}

	unlock := l.lockSession(sessionID)
	defer unlock()

	control := l.controlFor(sessionID)

	turnInput := agentkit.TurnInput{
		Message: req.Event.Message,
		Emit:    req.Emit,
		Session: sess,
		Control: control,
	}
	if _, err := ag.RunTurn(ctx, turnInput); err != nil {
		return agentkit.LoopResult{}, err
	}

	for {
		followUps, err := control.DrainFollowUps(ctx, l.followUpMode)
		if err != nil {
			return agentkit.LoopResult{}, err
		}
		if len(followUps) == 0 {
			break
		}
		for _, msg := range followUps {
			if _, err := ag.RunTurn(ctx, agentkit.TurnInput{
				Message: msg,
				Emit:    req.Emit,
				Session: sess,
				Control: control,
			}); err != nil {
				return agentkit.LoopResult{}, err
			}
		}
		if l.followUpMode == agentkit.FollowUpOneAtATime {
			break
		}
	}

	_ = agentID
	return agentkit.LoopResult{}, nil
}

func (l *Default) Steer(ctx context.Context, req agentkit.SessionControlRequest) error {
	ag, _, err := l.resolveAgent(req.AgentID)
	if err != nil {
		return err
	}
	sessionID := req.SessionID
	if sessionID == "" {
		sessionID = ag.Session().ID()
	}
	return l.controlFor(sessionID).Steer(ctx, req.Message)
}

func (l *Default) FollowUp(ctx context.Context, req agentkit.SessionControlRequest) error {
	ag, _, err := l.resolveAgent(req.AgentID)
	if err != nil {
		return err
	}
	sessionID := req.SessionID
	if sessionID == "" {
		sessionID = ag.Session().ID()
	}
	return l.controlFor(sessionID).FollowUp(ctx, req.Message)
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

func (l *Default) resolveSession(ctx context.Context, event agentkit.MessageEvent, ag agentkit.Agent) (agentkit.Session, agentkit.SessionID, error) {
	sessionID := event.SessionID
	if l.sessionStore != nil && sessionID != "" {
		sess, err := l.sessionStore.Get(ctx, sessionID)
		if err != nil {
			return nil, "", err
		}
		return sess, sessionID, nil
	}
	defaultSess := ag.Session()
	if sessionID == "" {
		sessionID = defaultSess.ID()
	}
	return defaultSess, sessionID, nil
}

func (l *Default) lockSession(id agentkit.SessionID) func() {
	v, _ := l.sessionLocks.LoadOrStore(id, &sync.Mutex{})
	mu := v.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}

type agentNotFoundError struct{ id agentkit.AgentID }

func (e agentNotFoundError) Error() string       { return "agent not found: " + string(e.id) }
func errAgentNotFound(id agentkit.AgentID) error { return agentNotFoundError{id} }
