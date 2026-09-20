package subagent

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/lengzhao/agentkit"
	capschedule "github.com/lengzhao/agentkit/cap/schedule"
	capsession "github.com/lengzhao/agentkit/cap/session"
	"github.com/lengzhao/agentkit/cap/subagent"
	captelemetry "github.com/lengzhao/agentkit/cap/telemetry"
	"github.com/lengzhao/agentkit/runtime/rctx"
	"github.com/lengzhao/agentkit/runtime/session/derive"
	"github.com/lengzhao/agentkit/runtime/session/sessevents"
	"github.com/lengzhao/agentkit/runtime/subagent/definition"
	rttelemetry "github.com/lengzhao/agentkit/runtime/telemetry"
	"github.com/lengzhao/pluginkit"
)

func init() {
	pluginkit.Register("subagent/loop-agent", NewLoopAgent)
}

// LoopAgentEntry configures one delegatable Loop agent.
type LoopAgentEntry struct {
	Name           string `json:"name"`
	Description    string `json:"description"`
	Agent          string `json:"agent"`
	Async          bool   `json:"async,omitempty"`
	TimeoutSeconds int    `json:"timeoutSeconds,omitempty"`
}

// LoopAgentConfig configures subagent/loop-agent.
type LoopAgentConfig struct {
	// Agents lists Loop-backed subagents exposed to the delegate tool.
	Agents []LoopAgentEntry `json:"agents,omitempty"`
	// TimeoutSeconds is the default wall clock for one delegation.
	TimeoutSeconds int `json:"timeoutSeconds,omitempty"`
	// MaxConcurrentJobsPerSession limits async jobs per parent session (channel semaphore).
	// Zero means no limit.
	MaxConcurrentJobsPerSession int `json:"maxConcurrentJobsPerSession,omitempty"`
}

// LoopAgentDeps holds injected capabilities for Loop-backed delegation.
type LoopAgentDeps struct {
	SessionStore agentkit.SessionStore `json:"sessionStore"`
	Agents       []agentkit.Agent      `json:"agents"`
	Telemetry    captelemetry.Exporter `json:"telemetry,omitempty"`
}

// LoopAgentSpawner delegates to configured Loop agents (e.g. agent/acp-remote).
type jobGate struct {
	slots chan struct{}
}

type LoopAgentSpawner struct {
	entries                    []LoopAgentEntry
	defaultTO                  time.Duration
	maxConcurrentJobsPerParent int
	store                      agentkit.SessionStore
	agents                     map[agentkit.AgentID]agentkit.Agent
	telemetry                  captelemetry.Exporter
	submit                     capschedule.SubmitFunc
	jobGates                   sync.Map // parentSession -> *jobGate
}

var _ subagent.Spawner = (*LoopAgentSpawner)(nil)
var _ subagent.SubmitBinder = (*LoopAgentSpawner)(nil)

// NewLoopAgent registers subagent/loop-agent: delegate to a Loop agent instance and return only its conclusion.
func NewLoopAgent(cfg LoopAgentConfig, deps LoopAgentDeps) (subagent.Spawner, error) {
	if deps.SessionStore == nil {
		return nil, fmt.Errorf("subagent/loop-agent requires sessionStore")
	}
	agents := make(map[agentkit.AgentID]agentkit.Agent, len(deps.Agents))
	for _, ag := range deps.Agents {
		if ag == nil {
			continue
		}
		agents[ag.ID()] = ag
	}
	var defaultTO time.Duration
	if cfg.TimeoutSeconds > 0 {
		defaultTO = time.Duration(cfg.TimeoutSeconds) * time.Second
	}
	exp := deps.Telemetry
	if exp == nil {
		exp = rttelemetry.Noop
	}
	return &LoopAgentSpawner{
		entries:                    cfg.Agents,
		defaultTO:                  defaultTO,
		maxConcurrentJobsPerParent: cfg.MaxConcurrentJobsPerSession,
		store:                      deps.SessionStore,
		agents:                     agents,
		telemetry:                  exp,
	}, nil
}

func (s *LoopAgentSpawner) BindSubmit(fn capschedule.SubmitFunc) {
	s.submit = fn
}

func (s *LoopAgentSpawner) Definitions(context.Context) ([]subagent.Definition, error) {
	return s.loadDefinitions(), nil
}

func (s *LoopAgentSpawner) Run(ctx context.Context, req subagent.Request) (subagent.Result, error) {
	name := strings.TrimSpace(req.Agent)
	task := strings.TrimSpace(req.Task)
	if name == "" {
		return subagent.Result{}, fmt.Errorf("subagent name is required")
	}
	if task == "" {
		return subagent.Result{}, fmt.Errorf("subagent task is required")
	}

	defs := s.loadDefinitions()
	def, ok := definition.Find(defs, name)
	if !ok {
		return subagent.Result{}, fmt.Errorf("unknown subagent %q; available: %s", name, namesOf(defs))
	}
	loopID := loopAgentID(def)
	ag, ok := s.agents[loopID]
	if !ok {
		return subagent.Result{}, fmt.Errorf("subagent %q maps to unknown loop agent %q", def.Name, loopID)
	}

	parentID := rctx.SessionIDFromContext(ctx)
	if parentID == "" {
		return subagent.Result{}, fmt.Errorf("delegation requires a parent session in context")
	}
	parentAgent := rctx.AgentIDFromContext(ctx)
	parent, err := sessevents.ParentSessionForDelegate(ctx, s.store, parentID)
	if err != nil {
		slog.Warn("subagent loop: resolve parent failed", "parent", parentID, "agent", def.Name, "err", err)
		return subagent.Result{}, err
	}
	parentEvents, err := derive.ReadAllEvents(ctx, parent)
	if err != nil {
		return subagent.Result{}, err
	}
	childID := agentkit.SessionID(rctx.ChildConversationID(string(parentID), def.Name, int64(capsession.LatestEventSeq(parentEvents))))
	jobID := string(childID)

	async := def.Async
	if req.Async != nil {
		async = *req.Async
	}
	slog.Info("subagent loop delegate", "agent", def.Name, "parent", parentID, "child", childID, "async", async)

	if async {
		if s.submit == nil {
			return subagent.Result{}, fmt.Errorf("async subagent %q: submit func not bound yet", def.Name)
		}
		if err := s.acquireJob(parentID); err != nil {
			return subagent.Result{}, err
		}
	}

	startData := sessevents.SubagentStartData{
		Agent:   def.Name,
		Session: string(childID),
		Task:    task,
		Async:   async,
		JobID:   jobID,
	}
	if err := sessevents.AppendSubagentStart(ctx, parent, parentAgent, startData); err != nil {
		slog.Warn("subagent loop: append start failed", "parent", parentID, "child", childID, "err", err)
		if async {
			s.releaseJob(parentID)
		}
		return subagent.Result{}, err
	}
	slog.Info("subagent loop: started", "agent", def.Name, "parent", parentID, "child", childID, "async", async)
	emitSubagentLifecycle(ctx, parentAgent, agentkit.EventSubagentStart, startData)

	if async {
		parentCtx := captureParentContext(ctx, parent)
		go s.runAsync(parentCtx, def, ag, task, childID, jobID, parentID, parentAgent)
		slog.Info("subagent loop: async returned", "agent", def.Name, "job", jobID)
		return subagent.Result{
			Agent:   def.Name,
			Session: string(childID),
			Status:  subagent.StatusRunning,
			Summary: "subagent started in the background; results will arrive in a follow-up turn",
			JobID:   jobID,
		}, nil
	}

	result, runErr, closer := s.runChild(ctx, def, ag, task, childID)
	// Sync delegation: the parent turn is still open and owns the progress card,
	// so drain the forwarded stream (blocking) before recording subagent/end so
	// the card is fully rendered before the tool result returns. closer is nil
	// for non-async children.
	if closer != nil {
		closer()
	}
	result = finalizeSubagentResult(result, runErr)
	end := sessevents.SubagentEndData{
		Agent:   def.Name,
		Session: string(childID),
		Status:  result.Status,
		Summary: result.Summary,
		Steps:   result.Steps,
		JobID:   jobID,
	}
	if runErr != nil {
		end.Error = runErr.Error()
	}
	if err := sessevents.AppendSubagentEnd(ctx, parent, parentAgent, end); err != nil {
		return subagent.Result{}, err
	}
	emitSubagentLifecycle(ctx, parentAgent, agentkit.EventSubagentEnd, end)
	result.JobID = jobID
	return result, runErr
}

func (s *LoopAgentSpawner) gateFor(parentID agentkit.SessionID) *jobGate {
	max := s.maxConcurrentJobsPerParent
	if max <= 0 {
		return nil
	}
	v, _ := s.jobGates.LoadOrStore(parentID, &jobGate{slots: make(chan struct{}, max)})
	return v.(*jobGate)
}

func (s *LoopAgentSpawner) acquireJob(parentID agentkit.SessionID) error {
	gate := s.gateFor(parentID)
	if gate == nil {
		return nil
	}
	select {
	case gate.slots <- struct{}{}:
		return nil
	default:
		return fmt.Errorf("parent session has too many concurrent async subagents (max %d)", s.maxConcurrentJobsPerParent)
	}
}

func (s *LoopAgentSpawner) releaseJob(parentID agentkit.SessionID) {
	gate := s.gateFor(parentID)
	if gate == nil {
		return
	}
	select {
	case <-gate.slots:
	default:
	}
}

type parentContext struct {
	conversation   agentkit.SessionID
	agentID        agentkit.AgentID
	envelope       agentkit.TurnEnvelope
	emit           agentkit.OutboundEmit
	parentSession  agentkit.Session
	sessionControl any
}

func captureParentContext(ctx context.Context, parent agentkit.Session) parentContext {
	env := rctx.EnvelopeFromContext(ctx)
	if env.Conversation == "" {
		if id := rctx.SessionIDFromContext(ctx); id != "" {
			env = env.WithConversation(string(id))
		}
	}
	agentID := rctx.AgentIDFromContext(ctx)
	var sessionControl any
	if v := ctx.Value(agentkit.KeySessionControl); v != nil {
		sessionControl = v
	}
	return parentContext{
		conversation:   agentkit.SessionID(env.Conversation),
		agentID:        agentID,
		envelope:       env,
		emit:           emitFromContext(ctx),
		parentSession:  parent,
		sessionControl: sessionControl,
	}
}

func (s *LoopAgentSpawner) runAsync(parent parentContext, def subagent.Definition, ag agentkit.Agent, task string, childID agentkit.SessionID, jobID string, parentID agentkit.SessionID, parentAgent agentkit.AgentID) {
	defer s.releaseJob(parentID)

	var result subagent.Result
	var runErr error
	var closer func()
	defer func() {
		if r := recover(); r != nil {
			runErr = fmt.Errorf("async subagent panic: %v", r)
			result = subagent.Result{
				Agent:   def.Name,
				Session: string(childID),
				Status:  "failed",
				Summary: runErr.Error(),
			}
			slog.Error("async subagent: panic", "job_id", jobID, "agent", def.Name, "panic", r)
		}
		// Deliver the follow-up turn BEFORE draining the forwarded progress-card
		// stream. The progress card is best-effort UI; the [subagent-complete]
		// follow-up is what actually resumes the parent agent. Draining first (the
		// old order) let a hung platform HTTP call block Close, which blocked
		// finishAsync, which blocked the follow-up — the parent agent never got
		// the response. See docs/guides/subagent.zh.md §6.1.
		s.finishAsync(parent, def, childID, jobID, parentID, parentAgent, result, runErr)
		if closer != nil {
			closer()
		}
	}()

	ctx := parent.asyncRunContext()

	result, runErr, closer = s.runChild(ctx, def, ag, task, childID)
	result = finalizeSubagentResult(result, runErr)
}

func (s *LoopAgentSpawner) finishAsync(parent parentContext, def subagent.Definition, childID agentkit.SessionID, jobID string, parentID agentkit.SessionID, parentAgent agentkit.AgentID, result subagent.Result, runErr error) {
	bg := context.Background()
	ctx := parent.asyncRunContext()
	ctx = rctx.WithAgentID(ctx, parentAgent)

	end := sessevents.SubagentEndData{
		Agent:   def.Name,
		Session: string(childID),
		Status:  result.Status,
		Summary: result.Summary,
		Steps:   result.Steps,
		JobID:   jobID,
	}
	if runErr != nil {
		end.Error = runErr.Error()
	}
	parentSess := parent.parentSession
	if parentSess == nil || parentSess.ID() != parentID {
		slog.Warn("async subagent: parent session not retained, store get", "job_id", jobID, "parent", parentID)
		var err error
		parentSess, err = s.store.Get(bg, parentID)
		if err != nil {
			slog.Error("async subagent: load parent session", "job_id", jobID, "err", err)
			parentSess = nil
		}
	} else {
		slog.Debug("async subagent: append end on retained parent session", "job_id", jobID, "parent", parentID)
	}
	if parentSess != nil {
		if err := sessevents.AppendSubagentEnd(bg, parentSess, parentAgent, end); err != nil {
			slog.Error("async subagent: append end", "job_id", jobID, "err", err)
		} else {
			emitSubagentLifecycle(ctx, parentAgent, agentkit.EventSubagentEnd, end)
		}
	}
	go func() {
		defer func() {
			if r := recover(); r != nil {
				slog.Error("async subagent: submit complete panic", "job_id", jobID, "agent", def.Name, "panic", r)
			}
		}()
		s.submitSubagentComplete(bg, parent, parentAgent, def.Name, jobID, result, runErr)
	}()
}

func finalizeSubagentResult(result subagent.Result, runErr error) subagent.Result {
	if runErr == nil {
		return result
	}
	if strings.TrimSpace(result.Summary) == "" {
		result.Summary = runErr.Error()
	}
	if result.Status == "" || result.Status == subagent.StatusStopped {
		result.Status = "failed"
	}
	return result
}

func (s *LoopAgentSpawner) submitSubagentComplete(ctx context.Context, parent parentContext, parentAgent agentkit.AgentID, agentName, jobID string, result subagent.Result, runErr error) {
	if s.submit == nil {
		return
	}
	text := formatSubagentComplete(agentName, jobID, result, runErr)
	parentEnv := parent.envelope
	if parentEnv.Conversation == "" {
		parentEnv = parentEnv.WithConversation(string(parent.conversation))
	}
	event := rctx.SyncMessageEvent(agentkit.MessageEvent{
		AgentID: parentAgent,
		Message: agentkit.ModelMessage{
			Role:    "user",
			Content: []agentkit.ContentPart{{Type: "text", Text: text}},
		},
		Metadata: map[string]any{
			"subagent_complete": true,
			"subagent_job_id":   jobID,
			"subagent_agent":    agentName,
			"subagent_status":   result.Status,
		},
	}, parentEnv)
	if err := s.submit(ctx, event); err != nil {
		slog.Error("async subagent: submit follow-up", "job_id", jobID, "err", err)
	}
}

func formatSubagentComplete(agentName, jobID string, result subagent.Result, runErr error) string {
	status := result.Status
	if runErr != nil && status != subagent.StatusCompleted && status != subagent.StatusBlocked {
		if status == "" || status == subagent.StatusStopped {
			status = "failed"
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "[subagent-complete agent=%s job=%s status=%s", agentName, jobID, status)
	if result.Session != "" {
		fmt.Fprintf(&b, " session=%s", result.Session)
	}
	b.WriteString("]\n")
	body := strings.TrimSpace(result.Summary)
	if body == "" && runErr != nil {
		body = runErr.Error()
	}
	if body != "" {
		b.WriteString(body)
	}
	return b.String()
}

func (s *LoopAgentSpawner) runChild(ctx context.Context, def subagent.Definition, ag agentkit.Agent, task string, childID agentkit.SessionID) (out subagent.Result, runErr error, closer func()) {
	out = subagent.Result{Agent: def.Name, Session: string(childID)}

	childCtx := context.WithValue(ctx, agentkit.KeyInSubagent, true)
	parentEnv := rctx.EnvelopeFromContext(ctx)
	childEnv := parentEnv.WithConversation(string(childID))
	childCtx = rctx.ApplyEnvelopeToContext(childCtx, childEnv)
	childCtx = rctx.WithAgentID(childCtx, ag.ID())
	// Keep parent KeySessionControl so ACP agents (cursor/claude) can use the same
	// permission broker while the parent turn is in delegate (sync or async child).
	childCtx = rttelemetry.WithExporter(childCtx, s.telemetry)

	turnMeta := s.childTurnMeta(childCtx, ag, task, childID)
	childCtx, endTurn := rttelemetry.BeginTurn(childCtx, turnMeta)
	childCtx = rttelemetry.WithTurnAccum(childCtx)
	defer func() {
		end := rttelemetry.TurnEndFromAccum(childCtx)
		if end.Output == "" {
			end.Output = strings.TrimSpace(out.Summary)
		}
		if end.Steps == 0 && out.Steps > 0 {
			end.Steps = out.Steps
		}
		if end.StopReason == "" && out.Status != "" {
			end.StopReason = out.Status
		}
		end.Err = runErr
		endTurn(end)
	}()

	timeout := s.timeoutFor(def)
	if timeout > 0 {
		var cancel context.CancelFunc
		childCtx, cancel = context.WithTimeout(childCtx, timeout)
		defer cancel()
	}

	emit, closeForward := forwardParentEmit(childCtx, emitFromContext(ctx))
	closer = closeForward
	runErr = ag.RunTurn(childCtx, agentkit.TurnInput{
		Message: agentkit.ModelMessage{
			Role:    "user",
			Content: []agentkit.ContentPart{{Type: "text", Text: task}},
		},
		Emit: emit,
	})

	sess, err := sessevents.LoadSession(ctx, s.store, childID)
	if err != nil {
		slog.Warn("subagent loop: load child session failed", "child", childID, "err", err)
		if runErr != nil {
			return out, runErr, closer
		}
		return out, err, closer
	}
	events, err := derive.ReadAllEvents(ctx, sess)
	if err != nil {
		if runErr != nil {
			return out, runErr, closer
		}
		return out, err, closer
	}
	out.Steps = capsession.StepCount(events, 0)
	if finish := capsession.FinishAfter(events, 0); finish != nil {
		out.Status = finish.Status
		out.Summary = finish.Summary
	} else {
		out.Status = subagent.StatusStopped
		out.Summary = capsession.LastAssistantText(events, 0)
	}
	return out, runErr, closer
}

func (s *LoopAgentSpawner) timeoutFor(def subagent.Definition) time.Duration {
	for _, entry := range s.entries {
		if strings.EqualFold(entry.Name, def.Name) && entry.TimeoutSeconds > 0 {
			return time.Duration(entry.TimeoutSeconds) * time.Second
		}
	}
	return s.defaultTO
}

func (s *LoopAgentSpawner) childTurnMeta(ctx context.Context, ag agentkit.Agent, task string, childID agentkit.SessionID) captelemetry.TurnMeta {
	env := rctx.EnvelopeFromContext(ctx)
	msg := agentkit.ModelMessage{
		Role:    "user",
		Content: []agentkit.ContentPart{{Type: "text", Text: task}},
	}
	return captelemetry.TurnMeta{
		TurnID:            uuid.NewString(),
		SessionID:         string(childID),
		DeliverySessionID: string(rctx.DeliveryFromEnvelope(env)),
		AgentID:           string(ag.ID()),
		PlatformID:        env.Route.Platform,
		UserID:            env.Actor.UserID,
		Input:             rttelemetry.FormatMessage(msg),
	}
}

func loopAgentID(def subagent.Definition) agentkit.AgentID {
	if id := strings.TrimSpace(def.LoopAgent); id != "" {
		return agentkit.AgentID(id)
	}
	return agentkit.AgentID(def.Name)
}

func (s *LoopAgentSpawner) loadDefinitions() []subagent.Definition {
	seen := make(map[string]struct{})
	var out []subagent.Definition
	for _, entry := range s.entries {
		name := strings.TrimSpace(entry.Name)
		if name == "" {
			continue
		}
		agentID := strings.TrimSpace(entry.Agent)
		if agentID == "" {
			agentID = name
		}
		if _, ok := s.agents[agentkit.AgentID(agentID)]; !ok {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, subagent.Definition{
			Name:        name,
			Description: strings.TrimSpace(entry.Description),
			Backend:     subagent.BackendLoop,
			LoopAgent:   agentID,
			Async:       entry.Async,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
