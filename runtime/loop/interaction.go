package loop

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/lengzhao/agentkit"
)

type pendingInteraction struct {
	id   string
	ch   chan agentkit.InteractionReply
	once sync.Once
}

func (p *pendingInteraction) deliver(reply agentkit.InteractionReply) bool {
	select {
	case p.ch <- reply:
		return true
	default:
		return false
	}
}

func (p *pendingInteraction) close() {
	p.once.Do(func() { close(p.ch) })
}

func (c *Control) RunInteraction(ctx context.Context, req agentkit.HumanInteraction) (agentkit.InteractionResult, error) {
	prompt := strings.TrimSpace(req.Prompt)
	if prompt == "" {
		return agentkit.InteractionResult{}, fmt.Errorf("interaction prompt is required")
	}
	req.Prompt = prompt
	if req.Kind == "" {
		req.Kind = agentkit.InteractionQuestion
	}
	if req.ID == "" {
		req.ID = newInteractionID()
	}

	emit, _ := ctx.Value(agentkit.KeyOutboundEmit).(agentkit.OutboundEmit)
	if emit == nil {
		return agentkit.InteractionResult{
			Selected: -1,
			Reason:   "no outbound channel for interaction",
		}, nil
	}

	if err := c.emitInteractionStart(ctx, emit, req); err != nil {
		return agentkit.InteractionResult{}, err
	}

	pending := &pendingInteraction{id: req.ID, ch: make(chan agentkit.InteractionReply, 1)}
	c.setPendingInteraction(pending)
	defer c.clearPendingInteraction()

	var result agentkit.InteractionResult
	if handler, ok := ctx.Value(agentkit.KeyInteractionHandler).(agentkit.InteractionHandler); ok && handler != nil {
		result = c.runSyncInteraction(ctx, req, handler)
	} else if async, ok := ctx.Value(agentkit.KeyAsyncInteraction).(bool); ok && async {
		result = c.waitAsyncInteraction(ctx, req)
	} else {
		result = agentkit.InteractionResult{
			Selected: -1,
			Reason:   "no interactive user on this platform",
		}
	}

	if err := c.emitInteractionEnd(ctx, emit, req.ID, result); err != nil {
		return agentkit.InteractionResult{}, err
	}
	return result, nil
}

func (c *Control) runSyncInteraction(ctx context.Context, req agentkit.HumanInteraction, handler agentkit.InteractionHandler) agentkit.InteractionResult {
	reply, err := handler.ReadInteractionReply(ctx, req)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return agentkit.InteractionResult{Selected: -1, Reason: "interaction abandoned: " + err.Error()}
		}
		return agentkit.InteractionResult{Selected: -1, Reason: err.Error()}
	}
	text := strings.TrimSpace(reply.Text)
	if text == "" {
		if req.Default == "" {
			return agentkit.InteractionResult{Selected: -1, Reason: "user gave an empty answer"}
		}
		text = req.Default
	}
	return agentkit.MatchInteractionOption(text, req.Options)
}

func (c *Control) waitAsyncInteraction(ctx context.Context, req agentkit.HumanInteraction) agentkit.InteractionResult {
	c.mu.Lock()
	pending := c.pending
	c.mu.Unlock()
	if pending == nil {
		return agentkit.InteractionResult{Selected: -1, Reason: "no interactive user on this platform"}
	}

	select {
	case <-ctx.Done():
		return agentkit.InteractionResult{Selected: -1, Reason: "interaction abandoned: " + ctx.Err().Error()}
	case reply, ok := <-pending.ch:
		if !ok {
			return agentkit.InteractionResult{Selected: -1, Reason: "interaction closed"}
		}
		text := strings.TrimSpace(reply.Text)
		if text == "" {
			if req.Default == "" {
				return agentkit.InteractionResult{Selected: -1, Reason: "user gave an empty answer"}
			}
			text = req.Default
		}
		return agentkit.MatchInteractionOption(text, req.Options)
	}
}

func (c *Control) DeliverInteractionReply(sessionID agentkit.SessionID, interactionID, text string) bool {
	_ = sessionID
	c.mu.Lock()
	pending := c.pending
	c.mu.Unlock()
	if pending == nil {
		return false
	}
	if interactionID != "" && pending.id != interactionID {
		return false
	}
	return pending.deliver(agentkit.InteractionReply{Text: text})
}

func (c *Control) setPendingInteraction(p *pendingInteraction) {
	c.mu.Lock()
	c.pending = p
	c.mu.Unlock()
}

func (c *Control) clearPendingInteraction() {
	c.mu.Lock()
	if c.pending != nil {
		c.pending.close()
	}
	c.pending = nil
	c.mu.Unlock()
}

func (c *Control) emitInteractionStart(ctx context.Context, emit agentkit.OutboundEmit, req agentkit.HumanInteraction) error {
	sessionID, _ := ctx.Value(agentkit.KeySessionID).(agentkit.SessionID)
	agentID, _ := ctx.Value(agentkit.KeyAgentID).(agentkit.AgentID)
	platformID, _ := ctx.Value(agentkit.KeyPlatformID).(string)
	userID, _ := ctx.Value(agentkit.KeyUserID).(string)
	return emit(ctx, agentkit.OutboundEvent{
		SessionID:  sessionID,
		AgentID:    agentID,
		PlatformID: platformID,
		UserID:     userID,
		Type:       agentkit.EventInteractionStart,
		Data: agentkit.MarshalOutboundData(agentkit.InteractionStartPayload{
			ID:       req.ID,
			Kind:     req.Kind,
			Prompt:   req.Prompt,
			Options:  req.Options,
			Default:  req.Default,
			Multiple: req.Multiple,
		}),
	})
}

func (c *Control) emitInteractionEnd(ctx context.Context, emit agentkit.OutboundEmit, id string, result agentkit.InteractionResult) error {
	sessionID, _ := ctx.Value(agentkit.KeySessionID).(agentkit.SessionID)
	agentID, _ := ctx.Value(agentkit.KeyAgentID).(agentkit.AgentID)
	platformID, _ := ctx.Value(agentkit.KeyPlatformID).(string)
	userID, _ := ctx.Value(agentkit.KeyUserID).(string)
	return emit(ctx, agentkit.OutboundEvent{
		SessionID:  sessionID,
		AgentID:    agentID,
		PlatformID: platformID,
		UserID:     userID,
		Type:       agentkit.EventInteractionEnd,
		Data: agentkit.MarshalOutboundData(agentkit.InteractionEndPayload{
			ID:       id,
			Answered: result.Answered,
			Text:     result.Text,
			Selected: result.Selected,
			Reason:   result.Reason,
		}),
	})
}

func newInteractionID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "ix"
	}
	return hex.EncodeToString(b[:])
}

var _ agentkit.SessionInteraction = (*Control)(nil)
