package commandcompact

import (
	"context"
	"fmt"
	"io"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/command"
	"github.com/lengzhao/agentkit/cap/compaction"
	"github.com/lengzhao/pluginkit"
)

type CompactConfig struct {
	AgentID agentkit.AgentID `json:"agentId"`
}

type CompactDeps struct {
	SessionStore agentkit.SessionStore `json:"sessionStore"`
	Services     []compaction.Service  `json:"services"`
}

type Compact struct {
	agentID      agentkit.AgentID
	sessionStore agentkit.SessionStore
	services     []compaction.Service
}

func init() {
	pluginkit.Register("command/compact", NewCompact)
}

func NewCompact(cfg CompactConfig, deps CompactDeps) (command.Handler, error) {
	if deps.SessionStore == nil {
		return nil, fmt.Errorf("command/compact requires sessionStore dependency")
	}
	if len(deps.Services) == 0 {
		return nil, fmt.Errorf("command/compact requires at least one compaction service")
	}
	return &Compact{
		agentID:      cfg.AgentID,
		sessionStore: deps.SessionStore,
		services:     deps.Services,
	}, nil
}

func (c *Compact) Descriptor() command.Descriptor {
	return command.Descriptor{
		Name:        "compact",
		Description: "run session compaction now",
	}
}

func (c *Compact) Handle(ctx context.Context, req command.Request) (command.Result, error) {
	if req.SessionID == "" {
		return command.Result{}, fmt.Errorf("session id is required")
	}
	sess, err := c.sessionStore.Get(ctx, req.SessionID)
	if err != nil {
		return command.Result{}, err
	}
	messages, err := sess.DeriveMessages(ctx)
	if err != nil {
		return command.Result{}, err
	}
	agentID := c.agentID
	if agentID == "" {
		agentID = req.AgentID
	}
	_, applied, err := compaction.ApplyAll(ctx, c.services, compaction.Request{
		SessionID: req.SessionID,
		AgentID:   agentID,
		Session:   sess,
		Messages:  messages,
		Force:     true,
	})
	if err != nil {
		return command.Result{}, err
	}
	if applied == 0 {
		writeLine(req.ErrOut, "compaction: nothing to compact")
	} else {
		writeLine(req.ErrOut, fmt.Sprintf("compaction: applied %d service(s)", applied))
	}
	return command.Result{}, nil
}

func writeLine(w io.Writer, line string) {
	if w == nil {
		return
	}
	fmt.Fprintln(w, line)
}

var _ command.Handler = (*Compact)(nil)
