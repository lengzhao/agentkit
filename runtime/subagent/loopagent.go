package subagent

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lengzhao/agentkit"
	capschedule "github.com/lengzhao/agentkit/cap/schedule"
	"github.com/lengzhao/agentkit/cap/subagent"
	"github.com/lengzhao/agentkit/runtime/session"
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
}

// LoopAgentDeps holds injected capabilities for Loop-backed delegation.
type LoopAgentDeps struct {
	SessionStore agentkit.SessionStore `json:"sessionStore"`
	Agents       []agentkit.Agent      `json:"agents"`
}

// LoopAgentSpawner delegates to configured Loop agents (e.g. agent/acp-remote).
type LoopAgentSpawner struct {
	entries   []LoopAgentEntry
	defaultTO time.Duration
	store     agentkit.SessionStore
	agents    map[agentkit.AgentID]agentkit.Agent
	submit    capschedule.SubmitFunc
	running   sync.Map // parentSession -> count
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
	return &LoopAgentSpawner{
		entries:   cfg.Agents,
		defaultTO: defaultTO,
		store:     deps.SessionStore,
		agents:    agents,
	}, nil
}

func (s *LoopAgentSpawner) BindSubmit(fn capschedule.SubmitFunc) {
	s.submit = fn
}

func (s *LoopAgentSpawner) Definitions(context.Context) ([]subagent.Definition, error) {
	return s.loadDefinitions(), nil
}

func (s *LoopAgentSpawner) Run(ctx context.Context, req subagent.Request) (subagent.Result, error) {
	if ctx.Value(agentkit.KeyInSubagent) != nil {
		return subagent.Result{}, fmt.Errorf("a subagent cannot delegate further")
	}
	name := strings.TrimSpace(req.Agent)
	task := strings.TrimSpace(req.Task)
	if name == "" {
		return subagent.Result{}, fmt.Errorf("subagent name is required")
	}
	if task == "" {
		return subagent.Result{}, fmt.Errorf("subagent task is required")
	}

	defs := s.loadDefinitions()
	def, ok := findDefinition(defs, name)
	if !ok {
		return subagent.Result{}, fmt.Errorf("unknown subagent %q; available: %s", name, namesOf(defs))
	}
	loopID := loopAgentID(def)
	ag, ok := s.agents[loopID]
	if !ok {
		return subagent.Result{}, fmt.Errorf("subagent %q maps to unknown loop agent %q", def.Name, loopID)
	}

	parentID, _ := ctx.Value(agentkit.KeySessionID).(agentkit.SessionID)
	if parentID == "" {
		return subagent.Result{}, fmt.Errorf("delegation requires a parent session in context")
	}
	parentAgent, _ := ctx.Value(agentkit.KeyAgentID).(agentkit.AgentID)
	parent, err := s.store.Get(ctx, parentID)
	if err != nil {
		return subagent.Result{}, err
	}
	parentEvents, err := session.ReadAllEvents(ctx, parent)
	if err != nil {
		return subagent.Result{}, err
	}
	childID := agentkit.SessionID(fmt.Sprintf("sub:%s:%s:%d",
		parentID, def.Name, session.LatestEventSeq(parentEvents)))
	jobID := string(childID)

	async := def.Async
	if req.Async != nil {
		async = *req.Async
	}

	if async {
		if s.submit == nil {
			return subagent.Result{}, fmt.Errorf("async subagent %q: submit func not bound yet", def.Name)
		}
		if err := s.trackRunning(parentID); err != nil {
			return subagent.Result{}, err
		}
	}

	if err := session.AppendSubagentStart(ctx, parent, parentAgent, session.SubagentStartData{
		Agent:   def.Name,
		Session: string(childID),
		Task:    task,
		Async:   async,
		JobID:   jobID,
	}); err != nil {
		if async {
			s.untrackRunning(parentID)
		}
		return subagent.Result{}, err
	}

	if async {
		parentCtx := captureParentContext(ctx)
		go s.runAsync(parentCtx, def, ag, task, childID, jobID, parentID, parentAgent)
		return subagent.Result{
			Agent:   def.Name,
			Session: string(childID),
			Status:  subagent.StatusRunning,
			Summary: "subagent started in the background; results will arrive in a follow-up turn",
			JobID:   jobID,
		}, nil
	}

	result, runErr := s.runChild(ctx, def, ag, task, childID)
	end := session.SubagentEndData{
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
	if err := session.AppendSubagentEnd(ctx, parent, parentAgent, end); err != nil {
		return subagent.Result{}, err
	}
	result.JobID = jobID
	return result, runErr
}

func (s *LoopAgentSpawner) trackRunning(parentID agentkit.SessionID) error {
	const maxConcurrent = int32(1)
	v, _ := s.running.LoadOrStore(parentID, new(int32))
	count := v.(*int32)
	for {
		cur := atomic.LoadInt32(count)
		if cur >= maxConcurrent {
			return fmt.Errorf("parent session already has a running async subagent")
		}
		if atomic.CompareAndSwapInt32(count, cur, cur+1) {
			return nil
		}
	}
}

func (s *LoopAgentSpawner) untrackRunning(parentID agentkit.SessionID) {
	if v, ok := s.running.Load(parentID); ok {
		atomic.AddInt32(v.(*int32), -1)
	}
}

type parentContext struct {
	sessionID  agentkit.SessionID
	deliveryID agentkit.SessionID
	agentID    agentkit.AgentID
	platformID string
	userID     string
	emit       agentkit.OutboundEmit
}

func captureParentContext(ctx context.Context) parentContext {
	var out parentContext
	out.sessionID, _ = ctx.Value(agentkit.KeySessionID).(agentkit.SessionID)
	out.deliveryID, _ = ctx.Value(agentkit.KeyDeliverySessionID).(agentkit.SessionID)
	out.agentID, _ = ctx.Value(agentkit.KeyAgentID).(agentkit.AgentID)
	out.platformID, _ = ctx.Value(agentkit.KeyPlatformID).(string)
	out.userID, _ = ctx.Value(agentkit.KeyUserID).(string)
	out.emit = emitFromContext(ctx)
	return out
}

func (s *LoopAgentSpawner) runAsync(parent parentContext, def subagent.Definition, ag agentkit.Agent, task string, childID agentkit.SessionID, jobID string, parentID agentkit.SessionID, parentAgent agentkit.AgentID) {
	defer s.untrackRunning(parentID)
	bg := context.Background()
	ctx := context.WithValue(bg, agentkit.KeySessionID, parentID)
	ctx = context.WithValue(ctx, agentkit.KeyDeliverySessionID, parent.deliveryID)
	ctx = context.WithValue(ctx, agentkit.KeyAgentID, parentAgent)
	ctx = context.WithValue(ctx, agentkit.KeyPlatformID, parent.platformID)
	ctx = context.WithValue(ctx, agentkit.KeyUserID, parent.userID)
	ctx = context.WithValue(ctx, agentkit.KeyOutboundEmit, parent.emit)

	result, runErr := s.runChild(ctx, def, ag, task, childID)
	parentSess, err := s.store.Get(bg, parentID)
	if err != nil {
		slog.Error("async subagent: load parent session", "job_id", jobID, "err", err)
		return
	}
	end := session.SubagentEndData{
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
	if err := session.AppendSubagentEnd(bg, parentSess, parentAgent, end); err != nil {
		slog.Error("async subagent: append end", "job_id", jobID, "err", err)
		return
	}
	if s.submit == nil {
		return
	}
	text := formatSubagentComplete(def.Name, jobID, result)
	event := agentkit.MessageEvent{
		SessionID:         parentID,
		DeliverySessionID: parent.deliveryID,
		PlatformID:        parent.platformID,
		UserID:            parent.userID,
		AgentID:           parentAgent,
		Message: agentkit.ModelMessage{
			Role:    "user",
			Content: []agentkit.ContentPart{{Type: "text", Text: text}},
		},
		Metadata: map[string]any{
			"subagent_complete": true,
			"subagent_job_id":   jobID,
			"subagent_agent":    def.Name,
			"subagent_status":   result.Status,
		},
	}
	if err := s.submit(bg, event); err != nil {
		slog.Error("async subagent: submit follow-up", "job_id", jobID, "err", err)
	}
}

func formatSubagentComplete(agentName, jobID string, result subagent.Result) string {
	var b strings.Builder
	fmt.Fprintf(&b, "[subagent-complete agent=%s job=%s status=%s", agentName, jobID, result.Status)
	if result.Session != "" {
		fmt.Fprintf(&b, " session=%s", result.Session)
	}
	b.WriteString("]\n")
	if strings.TrimSpace(result.Summary) != "" {
		b.WriteString(result.Summary)
	}
	return b.String()
}

func (s *LoopAgentSpawner) runChild(ctx context.Context, def subagent.Definition, ag agentkit.Agent, task string, childID agentkit.SessionID) (out subagent.Result, runErr error) {
	out = subagent.Result{Agent: def.Name, Session: string(childID)}

	childCtx := context.WithValue(ctx, agentkit.KeyInSubagent, true)
	childCtx = context.WithValue(childCtx, agentkit.KeySessionID, childID)
	childCtx = context.WithValue(childCtx, agentkit.KeyAgentID, ag.ID())
	childCtx = context.WithValue(childCtx, agentkit.KeySessionControl, nil)

	timeout := s.timeoutFor(def)
	if timeout > 0 {
		var cancel context.CancelFunc
		childCtx, cancel = context.WithTimeout(childCtx, timeout)
		defer cancel()
	}

	emit := forwardParentEmit(ctx, emitFromContext(ctx))
	runErr = ag.RunTurn(childCtx, agentkit.TurnInput{
		Message: agentkit.ModelMessage{
			Role:    "user",
			Content: []agentkit.ContentPart{{Type: "text", Text: task}},
		},
		Emit: emit,
	})

	sess, err := s.store.Get(ctx, childID)
	if err != nil {
		if runErr != nil {
			return out, runErr
		}
		return out, err
	}
	events, err := session.ReadAllEvents(ctx, sess)
	if err != nil {
		if runErr != nil {
			return out, runErr
		}
		return out, err
	}
	out.Steps = session.StepCount(events, 0)
	if finish := session.FinishAfter(events, 0); finish != nil {
		out.Status = finish.Status
		out.Summary = finish.Summary
	} else {
		out.Status = subagent.StatusStopped
		out.Summary = session.LastAssistantText(events, 0)
	}
	return out, runErr
}

func (s *LoopAgentSpawner) timeoutFor(def subagent.Definition) time.Duration {
	for _, entry := range s.entries {
		if strings.EqualFold(entry.Name, def.Name) && entry.TimeoutSeconds > 0 {
			return time.Duration(entry.TimeoutSeconds) * time.Second
		}
	}
	return s.defaultTO
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
