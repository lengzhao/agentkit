// Package subagent runs child agents in-process.
//
// It lives under runtime/ rather than plugins/ because it must import
// runtime/agent to construct a child — the same reason agent/coding is
// registered from runtime/agent. Kind names are independent of package paths.
//
// # Why the child needs its own tool runtime
//
// Wiring the parent's tool runtime here would be a dependency cycle:
//
//	tools.default → tool.subagent.default → subagent.default → tools.default
//
// pluginkit rejects that at build time. So deps.tools must point at a sibling
// runtime instance, and since that instance does not mount tool/subagent, "only
// the main agent can delegate" stops being a policy and becomes structural.
package subagent

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/compaction"
	"github.com/lengzhao/agentkit/cap/subagent"
	"github.com/lengzhao/agentkit/cap/telemetry"
	"github.com/lengzhao/agentkit/cap/workspace"
	"github.com/lengzhao/agentkit/runtime/agent"
	"github.com/lengzhao/agentkit/runtime/session"
)

type Config struct {
	// Dirs are definition directories in precedence order; defaults to local:agents then global:agents.
	Dirs []string `json:"dirs,omitempty"`
	// MaxSteps is step cap for definitions that do not set their own; defaults to 20.
	MaxSteps int `json:"maxSteps,omitempty"`
	// TimeoutSeconds is wall clock for one delegation; 0 leaves the delegate tool's own timeout as the only bound.
	TimeoutSeconds int `json:"timeoutSeconds,omitempty"`
}

type Deps struct {
	Workspace    workspace.Service        `json:"workspace"`
	SessionStore agentkit.SessionStore    `json:"sessionStore"`
	LLM          agentkit.LLMProvider     `json:"llm"`
	Tools        agentkit.ToolRuntime     `json:"tools"`
	Prompt       agentkit.PromptAssembler `json:"prompt"`
	Hooks        agentkit.HookRuntime     `json:"hooks,omitempty"`
	Compaction   []compaction.Service     `json:"compaction,omitempty"`
}

const defaultMaxSteps = 20

type Spawner struct {
	dirs      []string
	maxSteps  int
	timeout   time.Duration
	workspace workspace.Service
	store     agentkit.SessionStore
	llm       agentkit.LLMProvider
	tools     agentkit.ToolRuntime
	prompt    agentkit.PromptAssembler
	hooks     agentkit.HookRuntime
	compact   []compaction.Service
}

// New registers subagent/inprocess: Run a child agent in-process from an agents/<name>.md definition and return only its conclusion.
//
// Best practices:
//   - deps.tools must be a sibling tools/runtime instance that does NOT mount tool/subagent: wiring the parent's runtime is a dependency cycle, and the separate instance is what makes 'only the main agent delegates' structural.
//   - Give the child a narrower tool set than the parent — read-only is the common case. Delegation is for context isolation, not for a second agent editing the same workspace.
//   - Pair with prompt/section/subagents so the parent can see who it may delegate to; the delegate tool's description is static and cannot list definitions read from disk.
//   - Raise the delegate entry in the parent's toolTimeouts: a child agent takes far longer than a normal tool call.
func New(cfg Config, deps Deps) (subagent.Spawner, error) {
	if deps.Workspace == nil {
		return nil, fmt.Errorf("subagent/inprocess requires workspace")
	}
	if deps.SessionStore == nil {
		return nil, fmt.Errorf("subagent/inprocess requires sessionStore")
	}
	if deps.LLM == nil {
		return nil, fmt.Errorf("subagent/inprocess requires llm")
	}
	if deps.Tools == nil {
		return nil, fmt.Errorf("subagent/inprocess requires a tools runtime that does not mount tool/subagent")
	}
	if deps.Prompt == nil {
		return nil, fmt.Errorf("subagent/inprocess requires prompt")
	}
	dirs := cfg.Dirs
	if len(dirs) == 0 {
		dirs = defaultDirs
	}
	maxSteps := cfg.MaxSteps
	if maxSteps <= 0 {
		maxSteps = defaultMaxSteps
	}
	var timeout time.Duration
	if cfg.TimeoutSeconds > 0 {
		timeout = time.Duration(cfg.TimeoutSeconds) * time.Second
	}
	return &Spawner{
		dirs:      dirs,
		maxSteps:  maxSteps,
		timeout:   timeout,
		workspace: deps.Workspace,
		store:     deps.SessionStore,
		llm:       deps.LLM,
		tools:     deps.Tools,
		prompt:    deps.Prompt,
		hooks:     deps.Hooks,
		compact:   deps.Compaction,
	}, nil
}

func (s *Spawner) Definitions(ctx context.Context) ([]subagent.Definition, error) {
	return loadDefinitions(ctx, s.workspace, s.dirs)
}

func (s *Spawner) Run(ctx context.Context, req subagent.Request) (subagent.Result, error) {
	name := strings.TrimSpace(req.Agent)
	task := strings.TrimSpace(req.Task)
	if name == "" {
		return subagent.Result{}, fmt.Errorf("subagent name is required")
	}
	if task == "" {
		return subagent.Result{}, fmt.Errorf("subagent task is required")
	}

	defs, err := s.Definitions(ctx)
	if err != nil {
		return subagent.Result{}, err
	}
	def, ok := findDefinition(defs, name)
	if !ok {
		return subagent.Result{}, fmt.Errorf("unknown subagent %q; available: %s", name, namesOf(defs))
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

	// The parent's current seq makes the id deterministic per call site and
	// unique within the parent session; session/store sanitizes the separators.
	childID := agentkit.SessionID(fmt.Sprintf("sub:%s:%s:%d",
		parentID, def.Name, session.LatestEventSeq(parentEvents)))

	startData := session.SubagentStartData{
		Agent:   def.Name,
		Session: string(childID),
		Task:    task,
	}
	if err := session.AppendSubagentStart(ctx, parent, parentAgent, startData); err != nil {
		return subagent.Result{}, err
	}
	if err := emitSubagentLifecycle(ctx, parentAgent, agentkit.EventSubagentStart, startData); err != nil {
		return subagent.Result{}, err
	}

	result, runErr := s.runChild(ctx, def, task, childID)
	end := session.SubagentEndData{
		Agent:   def.Name,
		Session: string(childID),
		Status:  result.Status,
		Summary: result.Summary,
		Steps:   result.Steps,
	}
	if runErr != nil {
		end.Error = runErr.Error()
	}
	if err := session.AppendSubagentEnd(ctx, parent, parentAgent, end); err != nil {
		return subagent.Result{}, err
	}
	if err := emitSubagentLifecycle(ctx, parentAgent, agentkit.EventSubagentEnd, end); err != nil {
		return subagent.Result{}, err
	}
	return result, runErr
}

func (s *Spawner) runChild(ctx context.Context, def subagent.Definition, task string, childID agentkit.SessionID) (out subagent.Result, runErr error) {
	out = subagent.Result{Agent: def.Name, Session: string(childID)}

	tools, err := newFilteredTools(ctx, s.tools, def.Tools, def.Skills)
	if err != nil {
		return out, fmt.Errorf("subagent %q: %w", def.Name, err)
	}
	maxSteps := def.MaxSteps
	if maxSteps <= 0 {
		maxSteps = s.maxSteps
	}
	child, err := agent.New(agent.Config{
		ID:       agentkit.AgentID("sub:" + def.Name),
		Model:    def.Model,
		MaxSteps: maxSteps,
	}, agent.Deps{
		SessionStore: s.store,
		LLM:          s.llm,
		Tools:        tools,
		Prompt:       &definitionPrompt{inner: s.prompt, body: def.Prompt, skills: def.Skills},
		Hooks:        s.hooks,
		Compaction:   s.compact,
		Workspace:    s.workspace,
	})
	if err != nil {
		return out, err
	}

	childCtx := context.WithValue(ctx, agentkit.KeyInSubagent, true)
	childCtx = context.WithValue(childCtx, agentkit.KeySessionID, childID)
	childCtx = context.WithValue(childCtx, agentkit.KeyAgentID, child.ID())
	// Drop the parent's turn control: steering and cancel reasons belong to the
	// parent's turn. turnControlFrom degrades to a no-op when the value is nil.
	childCtx = context.WithValue(childCtx, agentkit.KeySessionControl, nil)
	if s.timeout > 0 {
		var cancel context.CancelFunc
		childCtx, cancel = context.WithTimeout(childCtx, s.timeout)
		defer cancel()
	}

	childCtx, endSubagentObs := telemetry.BeginObservation(childCtx, telemetry.ObservationMetaFromContext(childCtx, telemetry.ObservationMeta{
		Name:  "subagent." + def.Name,
		Kind:  telemetry.KindSpan,
		Input: task,
		Scope: true,
	}))
	defer func() {
		end := telemetry.ObservationEnd{Output: out.Summary}
		if runErr != nil {
			end.Err = runErr
		}
		endSubagentObs(end)
	}()

	runErr = child.RunTurn(childCtx, agentkit.TurnInput{
		Message: agentkit.ModelMessage{
			Role:    "user",
			Content: []agentkit.ContentPart{{Type: "text", Text: task}},
		},
		Emit: forwardParentEmit(ctx, emitFromContext(ctx)),
	})

	// Read the outcome even when the turn failed: a child that worked for ten
	// steps and then hit a provider error still has a partial answer worth
	// carrying back.
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

func findDefinition(defs []subagent.Definition, name string) (subagent.Definition, bool) {
	for _, def := range defs {
		if strings.EqualFold(def.Name, name) {
			return def, true
		}
	}
	return subagent.Definition{}, false
}

// namesOf renders the available agents for an error the model can act on: it
// cannot edit a definition file, but it can retry with a name that exists.
func namesOf(defs []subagent.Definition) string {
	if len(defs) == 0 {
		return "(none defined)"
	}
	names := make([]string, 0, len(defs))
	for _, def := range defs {
		names = append(names, def.Name)
	}
	return strings.Join(names, ", ")
}
