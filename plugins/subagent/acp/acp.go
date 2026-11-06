// Package acp registers subagent/acp: synchronous delegation to explicitly
// configured ACP backends. It needs neither MCP nor a model-driven parent loop.
package acp

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/lengzhao/agentkit"
	capsubagent "github.com/lengzhao/agentkit/cap/subagent"
	"github.com/lengzhao/agentkit/plugins/agent/acpremote"
	"github.com/lengzhao/agentkit/runtime/session/sessevents"
	rtsubagent "github.com/lengzhao/agentkit/runtime/subagent"
	"github.com/lengzhao/pluginkit"
)

// Entry is supplied by trusted host configuration. Role discovery, routing and
// persona-file conventions belong to the host, not this execution adapter.
type Entry struct {
	Name        string           `json:"name"`
	Description string           `json:"description"`
	ACP         acpremote.Config `json:"acp"`
}
type Config struct {
	Agents         []Entry `json:"agents"`
	TimeoutSeconds int     `json:"timeoutSeconds,omitempty"`
}

// Deps reuses the ACP backend capabilities. SessionStore is required here;
// SessionMCP and SessionRestore remain optional and are independently supplied.
type Deps = acpremote.Deps

func (c *Config) Validate() error {
	if c.TimeoutSeconds < 0 {
		return fmt.Errorf("subagent/acp timeoutSeconds must not be negative")
	}
	if len(c.Agents) == 0 {
		return fmt.Errorf("subagent/acp requires agents")
	}
	names := map[string]bool{}
	ids := map[agentkit.AgentID]bool{}
	for _, e := range c.Agents {
		name := strings.TrimSpace(e.Name)
		key := strings.ToLower(name)
		if name == "" || strings.TrimSpace(e.Description) == "" {
			return fmt.Errorf("subagent/acp requires agent name and description")
		}
		if names[key] {
			return fmt.Errorf("subagent/acp duplicate name %q", name)
		}
		names[key] = true
		cfg := e.ACP
		if cfg.ID == "" {
			cfg.ID = agentkit.AgentID(name)
		}
		if ids[cfg.ID] {
			return fmt.Errorf("subagent/acp duplicate ACP id %q", cfg.ID)
		}
		ids[cfg.ID] = true
		if cfg.AutoApprove {
			return fmt.Errorf("subagent/acp %q: autoApprove is not supported; permissions belong to the parent", name)
		}
		if err := cfg.Validate(); err != nil {
			return fmt.Errorf("subagent/acp %q: %w", name, err)
		}
	}
	return nil
}

type Spawner struct {
	inner    capsubagent.Spawner
	cancel   context.CancelFunc
	lifetime context.Context
	mu       sync.Mutex
	closed   bool
	once     sync.Once
	hooks    []func()
}

func init()                                       { pluginkit.Register("subagent/acp", New) }
func New(cfg Config, deps Deps) (*Spawner, error) { return newWithFactory(cfg, deps, acpremote.New) }

func newWithFactory(cfg Config, deps Deps, factory func(acpremote.Config, acpremote.Deps) (agentkit.Agent, error)) (*Spawner, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if deps.SessionStore == nil {
		return nil, fmt.Errorf("subagent/acp requires sessionStore")
	}
	// Delegation reads its conclusion from child history. ACP remote permits
	// event persistence to be disabled, but this adapter must always record it.
	if deps.SessionEvents == nil {
		deps.SessionEvents = sessevents.Default
	}
	life, cancel := context.WithCancel(context.Background())
	s := &Spawner{lifetime: life, cancel: cancel}
	entries := make([]rtsubagent.LoopAgentEntry, 0, len(cfg.Agents))
	agents := make([]agentkit.Agent, 0, len(cfg.Agents))
	for _, e := range cfg.Agents {
		name := strings.TrimSpace(e.Name)
		remote := e.ACP
		if remote.ID == "" {
			remote.ID = agentkit.AgentID(name)
		}
		// Delegations are fresh histories; do not retain a subprocess per completed task.
		remote.ReleaseSubprocessAfterTurn = true
		ag, err := factory(remote, deps)
		if err != nil {
			s.stop()
			return nil, fmt.Errorf("subagent/acp %q: %w", name, err)
		}
		agents = append(agents, ag)
		if provider, ok := ag.(agentkit.ShutdownHookProvider); ok {
			s.hooks = append(s.hooks, provider.ShutdownHooks()...)
		}
		entries = append(entries, rtsubagent.LoopAgentEntry{Name: name, Description: strings.TrimSpace(e.Description), Agent: string(ag.ID())})
	}
	timeout := cfg.TimeoutSeconds
	if timeout == 0 {
		timeout = capsubagent.DefaultDelegationTimeoutSeconds
	}
	inner, err := rtsubagent.NewLoopAgent(rtsubagent.LoopAgentConfig{Agents: entries, TimeoutSeconds: timeout, FreshSession: true}, rtsubagent.LoopAgentDeps{SessionStore: deps.SessionStore, Agents: agents})
	if err != nil {
		s.stop()
		return nil, err
	}
	s.inner = inner
	return s, nil
}

func (s *Spawner) Definitions(ctx context.Context) ([]capsubagent.Definition, error) {
	return s.inner.Definitions(ctx)
}
func (s *Spawner) Run(ctx context.Context, req capsubagent.Request) (capsubagent.Result, error) {
	if req.Async != nil && *req.Async {
		return capsubagent.Result{}, fmt.Errorf("subagent/acp supports synchronous delegation only")
	}
	s.mu.Lock()
	closed := s.closed
	s.mu.Unlock()
	if closed {
		return capsubagent.Result{}, fmt.Errorf("subagent/acp is closed")
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	stop := context.AfterFunc(s.lifetime, cancel)
	defer stop()
	if s.lifetime.Err() != nil {
		cancel()
	}
	if err := ctx.Err(); err != nil {
		return capsubagent.Result{}, err
	}
	return s.inner.Run(ctx, req)
}
func (s *Spawner) stop() {
	s.once.Do(func() {
		s.mu.Lock()
		s.closed = true
		s.mu.Unlock()
		s.cancel()
		for i := len(s.hooks) - 1; i >= 0; i-- {
			s.hooks[i]()
		}
	})
}
func (s *Spawner) ShutdownHooks() []func() { return []func(){s.stop} }

var _ capsubagent.Spawner = (*Spawner)(nil)
var _ agentkit.ShutdownHookProvider = (*Spawner)(nil)
