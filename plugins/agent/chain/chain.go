// Package chain registers agent/chain: run one inbound turn through several
// agents in sequence, sharing the session.
package chain

import (
	"context"
	"fmt"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/rctx"
	"github.com/lengzhao/pluginkit"
)

// Config configures agent/chain.
type Config struct {
	// ID is the chain agent id referenced by loop.defaultAgent or /agent use.
	ID agentkit.AgentID `json:"id"`
	// Nodes lists node agent ids (from deps.agents) executed in order for every turn.
	Nodes []agentkit.AgentID `json:"nodes"`
	// ContinueOnError keeps running later nodes after a node fails; default aborts the chain.
	ContinueOnError bool `json:"continueOnError,omitempty"`
}

// Deps holds the node agents; config.nodes references their ids.
type Deps struct {
	Agents []agentkit.Agent `json:"agents"`
}

// Runtime is a composite Agent that runs nodes sequentially within one dispatch.
type Runtime struct {
	id              agentkit.AgentID
	nodes           []agentkit.Agent
	continueOnError bool
}

func init() {
	pluginkit.Register("agent/chain", New)
}

// New registers agent/chain: sequential agent pipeline; every node runs a full turn on the shared session.
//
// Best practices:
//   - Nodes are regular agents (agent/coding, another agent/chain) injected via deps.agents.
//   - All nodes share the session: node N sees node N-1's messages via DeriveMessages.
//   - The inbound user message is recorded once, on the first node's turn; later nodes
//     receive an empty message, so agents that require a non-empty prompt
//     (agent/acp-remote) can only be the first node.
//   - Keep routing-style nodes small (own model, few maxSteps); skip/repeat policies belong to wrapper plugins, not chain.
func New(cfg Config, deps Deps) (agentkit.Agent, error) {
	if cfg.ID == "" {
		return nil, fmt.Errorf("agent/chain requires config.id")
	}
	if len(cfg.Nodes) == 0 {
		return nil, fmt.Errorf("agent/chain requires at least one node")
	}
	byID := make(map[agentkit.AgentID]agentkit.Agent, len(deps.Agents))
	for _, ag := range deps.Agents {
		if ag != nil {
			byID[ag.ID()] = ag
		}
	}
	nodes := make([]agentkit.Agent, 0, len(cfg.Nodes))
	for _, id := range cfg.Nodes {
		ag, ok := byID[id]
		if !ok {
			return nil, fmt.Errorf("agent/chain node %q is not in deps.agents", id)
		}
		nodes = append(nodes, ag)
	}
	return &Runtime{id: cfg.ID, nodes: nodes, continueOnError: cfg.ContinueOnError}, nil
}

func (c *Runtime) ID() agentkit.AgentID { return c.id }

// AgentCatalogEntry describes the chain for /agent help output.
func (c *Runtime) AgentCatalogEntry() string {
	names := ""
	for i, n := range c.nodes {
		if i > 0 {
			names += " -> "
		}
		names += string(n.ID())
	}
	return fmt.Sprintf("chain: %s", names)
}

func (c *Runtime) RunTurn(ctx context.Context, input agentkit.TurnInput) error {
	if rctx.SessionIDFromContext(ctx) == "" {
		return fmt.Errorf("agent/chain turn requires session id in context")
	}
	for i, node := range c.nodes {
		nodeCtx := rctx.WithAgentID(ctx, node.ID())
		nodeInput := input
		if i > 0 {
			// The inbound user message was recorded by the first node's turn;
			// later nodes continue from the shared session history.
			nodeInput.Message = agentkit.ModelMessage{}
		}
		if err := node.RunTurn(nodeCtx, nodeInput); err != nil {
			if !c.continueOnError {
				return fmt.Errorf("chain node %s: %w", node.ID(), err)
			}
		}
		if err := ctx.Err(); err != nil {
			return err
		}
	}
	return nil
}

var (
	_ agentkit.Agent             = (*Runtime)(nil)
	_ agentkit.AgentCatalogEntry = (*Runtime)(nil)
)
