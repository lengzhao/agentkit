package loop

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/lengzhao/agentkit"
	capexecution "github.com/lengzhao/agentkit/cap/execution"
	capllm "github.com/lengzhao/agentkit/cap/llm"
	"github.com/lengzhao/agentkit/cap/permission"
	capschedule "github.com/lengzhao/agentkit/cap/schedule"
	capsession "github.com/lengzhao/agentkit/cap/session"
	captelemetry "github.com/lengzhao/agentkit/cap/telemetry"
	"github.com/lengzhao/agentkit/cap/workspace"
	"github.com/lengzhao/agentkit/runtime/rctx"
	"github.com/lengzhao/agentkit/runtime/session/sessctx"
	rttelemetry "github.com/lengzhao/agentkit/runtime/telemetry"
)

type Config struct {
	// DefaultAgent is agent id used when the event names none; defaults to the single configured agent.
	DefaultAgent agentkit.AgentID `json:"defaultAgent"`
	// FollowUpMode is how follow-up messages are drained after a turn ends.
	FollowUpMode agentkit.FollowUpMode `json:"followUpMode"`
}

type Deps struct {
	Agents    []agentkit.Agent      `json:"agents"`
	Telemetry captelemetry.Exporter `json:"telemetry,omitempty"`
	// SessionStore, Workspace and ModelCatalog are optional and only serve the
	// catalog slash commands (/agent, /model) the loop contributes via
	// agentkit.CommandProvider; turn dispatch never touches them.
	SessionStore agentkit.SessionStore `json:"sessionStore,omitempty"`
	Workspace    workspace.Service     `json:"workspace,omitempty"`
	ModelCatalog capllm.ModelCatalog   `json:"modelCatalog,omitempty"`
}

// SetDefaults implements pluginkit.Defaulter.
func (c *Config) SetDefaults() {
	if c.FollowUpMode == "" {
		c.FollowUpMode = agentkit.FollowUpOneAtATime
	}
}

// Validate implements pluginkit.Validator.
func (c *Config) Validate() error {
	switch c.FollowUpMode {
	case agentkit.FollowUpOneAtATime, agentkit.FollowUpAll:
		return nil
	default:
		return fmt.Errorf("loop followUpMode must be %q or %q, got %q",
			agentkit.FollowUpOneAtATime, agentkit.FollowUpAll, c.FollowUpMode)
	}
}

type Default struct {
	agents          map[agentkit.AgentID]agentkit.Agent
	defaultAgent    agentkit.AgentID
	followUpMode    agentkit.FollowUpMode
	telemetry       captelemetry.Exporter
	sessionStore    agentkit.SessionStore
	workspace       workspace.Service
	modelCatalog    capllm.ModelCatalog
	controlsMu      sync.Mutex
	controlsStopped bool
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
	cfg.SetDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	defaultID := cfg.DefaultAgent
	if defaultID == "" && len(deps.Agents) > 0 {
		defaultID = deps.Agents[0].ID()
	}
	mode := cfg.FollowUpMode
	if len(deps.Agents) == 0 {
		return nil, fmt.Errorf("loop requires at least one agent")
	}
	exp := deps.Telemetry
	if exp == nil {
		exp = rttelemetry.Noop
	}
	return &Default{
		agents:       agents,
		defaultAgent: defaultID,
		followUpMode: mode,
		telemetry:    exp,
		sessionStore: deps.SessionStore,
		workspace:    deps.Workspace,
		modelCatalog: deps.ModelCatalog,
	}, nil
}

// Commands implements agentkit.CommandProvider: the loop owns the agent
// catalog, so it contributes /agent and, when any agent exposes local model
// configuration, /model. commands/registry collects them through the generic
// WireContributions channel; turn dispatch is unaffected when the optional
// command deps are absent.
func (l *Default) Commands() []agentkit.Command {
	agents := l.Agents()
	commands := []agentkit.Command{AgentCommand(agents, l.sessionStore, l.defaultAgent, l.workspace)}
	for _, ag := range agents {
		// Match the capability already used to inspect local model defaults. ACP
		// agents own their configuration commands and do not expose this method.
		if _, ok := ag.(interface{ ConfiguredModel() string }); ok {
			commands = append(commands, ModelCommand(agents, l.sessionStore, l.defaultAgent, l.workspace, l.modelCatalog))
			break
		}
	}
	return commands
}

// DefaultAgentID returns the loop fallback agent when no session bind is set.
func (l *Default) DefaultAgentID() agentkit.AgentID {
	return l.defaultAgent
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
	ag, agentID, err := l.resolveAgent(req.Event.Envelope.AgentID)
	if err != nil {
		return err
	}
	conversation := rctx.ConversationFromLoopRequest(req)
	if conversation == "" {
		return fmt.Errorf("loop request requires conversation")
	}

	// Per-session turn ordering is enforced by runner/scheduler session queues; no mutex here.

	l.markSessionBusy(conversation, true)
	defer l.markSessionBusy(conversation, false)

	control := l.controlFor(conversation)
	capab := permissionCapability(req.Capability)
	control.setTurnCapability(capab)
	ctx = withTurnContext(ctx, req.Event.Envelope.WithAgentID(agentID), control, req.Emit)
	ctx = rttelemetry.WithExporter(ctx, l.telemetry)

	turnInput := agentkit.TurnInput{
		Message: req.Event.Message,
		Emit:    req.Emit,
	}
	if err := l.runTurn(ctx, req, agentID, ag, turnInput); err != nil {
		return err
	}
	// Inbound attachments belong to the first turn's input message; steering and
	// follow-up turns reuse req but must not re-emit the attachment span.
	req.Event.Attachments = nil

	for {
		if steered := control.PopSteering(); len(steered) > 0 {
			for i, entry := range steered {
				ackSteering(entry, capsession.SteerOutcomeNextTurn)
				if err := l.runTurn(ctx, req, agentID, ag, agentkit.TurnInput{
					Message: entry.Message,
					Emit:    req.Emit,
				}); err != nil {
					control.RestoreSteering(steered[i+1:])
					return err
				}
			}
			continue
		}
		followUps, err := control.DrainFollowUps(ctx, l.followUpMode)
		if err != nil {
			return err
		}
		if len(followUps) == 0 {
			if steered := control.PopSteering(); len(steered) > 0 {
				for i, entry := range steered {
					ackSteering(entry, capsession.SteerOutcomeNextTurn)
					if err := l.runTurn(ctx, req, agentID, ag, agentkit.TurnInput{
						Message: entry.Message,
						Emit:    req.Emit,
					}); err != nil {
						control.RestoreSteering(steered[i+1:])
						return err
					}
				}
				continue
			}
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
			if steered := control.PopSteering(); len(steered) > 0 {
				for i, entry := range steered {
					ackSteering(entry, capsession.SteerOutcomeNextTurn)
					if err := l.runTurn(ctx, req, agentID, ag, agentkit.TurnInput{
						Message: entry.Message,
						Emit:    req.Emit,
					}); err != nil {
						control.RestoreSteering(steered[i+1:])
						return err
					}
				}
				continue
			}
			break
		}
	}

	return nil
}

// ackSteering reports the terminal outcome for one drained steering entry. The
// message is about to run as its own turn. Report the routing decision before
// execution so a blocked or failed next turn cannot leave the outcome unresolved.
func ackSteering(entry agentkit.SteerEntry, outcome string) {
	if entry.Ack != nil {
		entry.Ack(outcome)
	}
}

func (l *Default) runTurn(ctx context.Context, req agentkit.LoopRequest, agentID agentkit.AgentID, ag agentkit.Agent, input agentkit.TurnInput) error {
	var contextErr error
	ctx, contextErr = sessctx.NextTurn(ctx)
	if contextErr != nil {
		return contextErr
	}
	turnID := uuid.NewString()
	// A caller-provided turn id (agentkit.MetadataTurnID) wins, so transports
	// that register per-turn resources before dispatch can correlate them.
	if id := turnIDFromMetadata(req.Event.Envelope.Metadata); id != "" {
		turnID = id
	}
	identity := capexecution.Identity{ID: uuid.NewString(), RootTurnID: turnID}
	ctx = rctx.WithExecutionIdentity(ctx, identity)
	if control, ok := ctx.Value(agentkit.KeySessionControl).(*Control); ok {
		endControlTurn := control.beginTurn(identity.ID)
		defer endControlTurn()
	}
	meta := captelemetry.TurnMeta{
		TurnID:            turnID,
		SessionID:         string(rctx.ConversationFromLoopRequest(req)),
		DeliverySessionID: string(rctx.DeliveryFromEnvelope(req.Event.Envelope)),
		AgentID:           string(agentID),
		Model:             agentTraceModel(ctx, ag),
		PlatformID:        req.Event.Envelope.Route.Instance,
		UserID:            req.Event.Envelope.Actor.UserID,
		WorkspaceKey:      req.Event.Envelope.Workspace,
		AttachmentSources: rttelemetry.JoinAttachmentSources(rttelemetry.AttachmentSources(input.Message)),
		Input:             rttelemetry.FormatMessage(input.Message),
	}
	slog.Info("turn start",
		"turn_id", turnID,
		"agent_id", agentID,
		"session_id", rctx.ConversationFromLoopRequest(req),
		"delivery_session_id", rctx.DeliveryFromEnvelope(req.Event.Envelope),
		"platform_id", req.Event.Envelope.Route.Instance,
		"user_id", req.Event.Envelope.Actor.UserID,
	)
	turnStarted := time.Now()
	ctx, endTurn := rttelemetry.BeginTurn(ctx, meta)
	ctx = rttelemetry.WithTurnAccum(ctx)
	// Emit a dedicated span for inbound file attachments so size/mime/path are
	// queryable in telemetry backends without re-parsing the trace input text.
	// Only platforms that save attachments to disk populate req.Event.Attachments
	// (with real on-disk sizes); entry points without it simply emit no span.
	if attrs := rttelemetry.AttachmentSpanAttrs(req.Event.Attachments); attrs != nil {
		_, endAttach := rttelemetry.BeginObservation(ctx, captelemetry.ObservationMeta{
			Name:       "inbound.attachments",
			Kind:       captelemetry.KindSpan,
			AgentID:    string(agentID),
			SessionID:  string(rctx.ConversationFromLoopRequest(req)),
			Attributes: attrs,
		})
		endAttach(captelemetry.ObservationEnd{})
	}
	var runErr error
	defer func() {
		end := rttelemetry.TurnEndFromAccum(ctx)
		end.Err = runErr
		endTurn(end)
		attrs := []any{
			"turn_id", turnID,
			"agent_id", agentID,
			"session_id", rctx.ConversationFromLoopRequest(req),
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
	turnInput.Emit = wrapTurnEndNotices(rttelemetry.WrapOutboundEmit(ctx, stampTurnID(input.Emit, turnID)))
	runErr = ag.RunTurn(ctx, turnInput)
	if isStepLimitError(runErr) {
		runErr = nil
	}
	return runErr
}

func (l *Default) Steer(ctx context.Context, msg agentkit.ModelMessage) error {
	sessionID, err := sessionIDFromContext(ctx)
	if err != nil {
		return err
	}
	return l.controlFor(sessionID).Steer(ctx, msg)
}

// SteerEntry queues one steering message with its outcome reporter. Transports
// that want per-message steering feedback call this instead of Steer; the
// runner picks it up through agentkit.SteerReporter.
func (l *Default) SteerEntry(ctx context.Context, entry agentkit.SteerEntry) error {
	sessionID, err := sessionIDFromContext(ctx)
	if err != nil {
		return err
	}
	return l.controlFor(sessionID).SteerEntry(ctx, entry)
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

func (l *Default) CancelAllInFlight(reason string) {
	if reason == "" {
		reason = "shutdown"
	}
	l.sessionBusy.Range(func(key, _ any) bool {
		sessionID, ok := key.(agentkit.SessionID)
		if !ok || sessionID == "" {
			return true
		}
		_ = l.controlFor(sessionID).Cancel(context.Background(), reason)
		return true
	})
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
	l.controlsMu.Lock()
	defer l.controlsMu.Unlock()
	if v, ok := l.sessionControls.Load(sessionID); ok {
		return v.(*Control)
	}
	c := NewControl()
	if l.controlsStopped {
		c.Stop()
	}
	l.sessionControls.Store(sessionID, c)
	return c
}

// Stop retires all session owners, including when in-flight turns are abandoned.
// Later cleanup calls are safe and cannot create a live owner after shutdown.
func (l *Default) Stop(_ context.Context) error {
	l.controlsMu.Lock()
	defer l.controlsMu.Unlock()
	l.controlsStopped = true
	l.sessionControls.Range(func(_, v any) bool {
		v.(*Control).Stop()
		return true
	})
	return nil
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

func withTurnContext(ctx context.Context, env agentkit.TurnEnvelope, control *Control, emit agentkit.OutboundEmit) context.Context {
	metadata := env.Metadata
	ctx = rctx.ApplyEnvelopeToContext(ctx, env)
	if len(metadata) > 0 {
		if capschedule.IsFireTurn(metadata) {
			ctx = context.WithValue(ctx, agentkit.KeyScheduleFireTurn, true)
		}
		if capschedule.IsFireStateless(metadata) {
			ctx = context.WithValue(ctx, agentkit.KeyScheduleStateless, true)
		}
	}
	if emit != nil {
		ctx = rctx.ContextWithOutboundEmit(ctx, emit)
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
	id := rctx.SessionIDFromContext(ctx)
	if id == "" {
		return "", fmt.Errorf("session id required in context")
	}
	return id, nil
}

type agentNotFoundError struct{ id agentkit.AgentID }

func (e agentNotFoundError) Error() string       { return "agent not found: " + string(e.id) }
func errAgentNotFound(id agentkit.AgentID) error { return agentNotFoundError{id} }
