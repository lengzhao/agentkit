package loop

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/permission"
	rtpermission "github.com/lengzhao/agentkit/runtime/permission"
	"github.com/lengzhao/agentkit/runtime/session"
)

type pendingPermission struct {
	requestID    string
	askedBy      string
	scope        permission.AnswerScope
	replies      chan permission.Reply
	superseded   chan struct{}
	closeReplies sync.Once
	closeSuper   sync.Once
}

func (p *pendingPermission) deliver(reply permission.Reply) bool {
	select {
	case p.replies <- reply:
		return true
	default:
		return false
	}
}

func (p *pendingPermission) finish() {
	p.closeReplies.Do(func() { close(p.replies) })
}

func (p *pendingPermission) signalSuperseded() {
	p.closeSuper.Do(func() { close(p.superseded) })
}

func (c *Control) Await(ctx context.Context, req permission.Request) (permission.Result, error) {
	capab := rtpermission.CapabilityFrom(ctx)
	if !capab.Interactive {
		return rtpermission.NoHuman(req, "platform has no interactive user"), nil
	}
	if err := validateRequest(req); err != nil {
		return permission.Result{}, err
	}
	if req.ID == "" {
		req.ID = newPermissionRequestID()
	}
	if req.AskedBy == "" {
		if userID := session.UserIDFromContext(ctx); userID != "" {
			req.AskedBy = userID
		}
	}
	if req.Timeout == 0 {
		req.Timeout = rtpermission.EffectiveTimeout(req, capab)
	}
	if req.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, req.Timeout)
		defer cancel()
	}

	switch req.Kind {
	case permission.KindAllowDeny, permission.KindQuestion:
		return c.awaitOne(ctx, req, capab)
	default:
		return permission.Result{}, fmt.Errorf("unsupported permission kind %q", req.Kind)
	}
}

func validateRequest(req permission.Request) error {
	switch req.Kind {
	case permission.KindAllowDeny:
		if req.ToolCall == nil {
			return fmt.Errorf("allow_deny permission requires toolCall")
		}
	case permission.KindQuestion:
		if req.Question == nil {
			return fmt.Errorf("question permission requires question")
		}
	default:
		return fmt.Errorf("unsupported permission kind %q", req.Kind)
	}
	return nil
}

func (c *Control) awaitOne(ctx context.Context, req permission.Request, capab permission.Capability) (permission.Result, error) {
	emit := agentkit.OutboundEmitFromContext(ctx)
	if emit == nil {
		return rtpermission.NoHuman(req, "no outbound channel for permission"), nil
	}

	pending, err := c.registerPermissionPending(req, capab)
	if err != nil {
		return permission.Result{}, err
	}
	defer c.clearPermissionPending()

	if err := c.emitPermissionRequest(ctx, emit, req); err != nil {
		return permission.Result{}, err
	}

	result, waitErr := c.waitPermissionReply(ctx, req, pending, capab)
	if emitErr := c.emitPermissionResolved(ctx, emit, req.ID, result); emitErr != nil {
		slog.Error("emit permission/resolved failed", "request_id", req.ID, "err", emitErr)
	}
	if waitErr != nil {
		return permission.Result{}, waitErr
	}
	return result, nil
}

func (c *Control) waitPermissionReply(ctx context.Context, req permission.Request, pending *pendingPermission, capab permission.Capability) (permission.Result, error) {
	select {
	case <-ctx.Done():
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return rtpermission.TimedOut(req), nil
		}
		return rtpermission.Cancelled(req, "permission abandoned: "+ctx.Err().Error()), nil
	case <-pending.superseded:
		return rtpermission.Superseded(req, "superseded by new inbound message"), nil
	case reply, ok := <-pending.replies:
		if !ok {
			return rtpermission.Cancelled(req, "permission closed"), nil
		}
		return c.resolvePermissionReply(req, reply, capab)
	}
}

func (c *Control) resolvePermissionReply(req permission.Request, reply permission.Reply, capab permission.Capability) (permission.Result, error) {
	if reply.Cancelled {
		return rtpermission.Cancelled(req, "user declined"), nil
	}
	switch req.Kind {
	case permission.KindAllowDeny:
		matched := rtpermission.MatchAllowDeny(reply)
		if !matched.Recognized {
			return permission.Result{
				Outcome: permission.OutcomeResolved,
				Allow:   false,
				Reason:  rtpermission.UnrecognizedAllowDenyReason(reply),
			}, nil
		}
		out := permission.Result{Outcome: permission.OutcomeResolved, Allow: matched.Allow}
		if len(matched.UpdatedInput) > 0 {
			out.UpdatedInput = matched.UpdatedInput
		}
		return out, nil
	case permission.KindQuestion:
		q := *req.Question
		answer := rtpermission.MatchReply(reply, q)
		if !capab.MultiSelect && len(answer.Selected) > 1 {
			answer.Selected = answer.Selected[:1]
			if len(q.Options) > 0 && answer.Selected[0] >= 0 && answer.Selected[0] < len(q.Options) {
				answer.Text = q.Options[answer.Selected[0]].Label
			}
		}
		return permission.Result{
			Outcome: permission.OutcomeResolved,
			Answer:  &answer,
		}, nil
	default:
		return permission.Result{}, fmt.Errorf("unsupported permission kind %q", req.Kind)
	}
}

func (c *Control) registerPermissionPending(req permission.Request, capab permission.Capability) (*pendingPermission, error) {
	scope := capab.AnswerScope
	if scope == "" {
		scope = permission.ScopeAsker
	}
	pending := &pendingPermission{
		requestID:  req.ID,
		askedBy:    req.AskedBy,
		scope:      scope,
		replies:    make(chan permission.Reply, 1),
		superseded: make(chan struct{}),
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.permissionPending != nil {
		return nil, fmt.Errorf("permission pending already exists: %s", c.permissionPending.requestID)
	}
	c.permissionPending = pending
	return pending, nil
}

func (c *Control) clearPermissionPending() {
	c.mu.Lock()
	if c.permissionPending != nil {
		c.permissionPending.finish()
	}
	c.permissionPending = nil
	c.mu.Unlock()
}

func (c *Control) DeliverPermissionReply(_ agentkit.SessionID, reply permission.Reply) bool {
	if strings.TrimSpace(reply.RequestID) == "" {
		return false
	}
	c.mu.Lock()
	pending := c.permissionPending
	c.mu.Unlock()
	if pending == nil {
		return false
	}
	if pending.requestID != reply.RequestID {
		return false
	}
	if pending.scope == permission.ScopeAsker && pending.askedBy != "" && reply.UserID != "" && reply.UserID != pending.askedBy {
		return false
	}
	return pending.deliver(reply)
}

func (c *Control) SupersedePending(_ agentkit.SessionID, reason string) bool {
	_ = reason
	c.mu.Lock()
	pending := c.permissionPending
	c.mu.Unlock()
	if pending == nil {
		return false
	}
	pending.signalSuperseded()
	return true
}

func (l *Default) TryDeliverPermission(event agentkit.MessageEvent) bool {
	conversation := session.ConversationFromEvent(event)
	if conversation == "" || len(event.Reply) == 0 {
		return false
	}
	reply, err := rtpermission.DecodeReply(event.Reply)
	if err != nil {
		return false
	}
	return l.controlFor(conversation).DeliverPermissionReply(conversation, reply)
}

func (l *Default) SupersedePendingForInbound(event agentkit.MessageEvent) {
	conversation := session.ConversationFromEvent(event)
	if conversation == "" || event.Message.Role == "" || len(event.Reply) > 0 {
		return
	}
	l.controlFor(conversation).SupersedePending(conversation, "superseded by new inbound message")
}

func (c *Control) emitPermissionRequest(ctx context.Context, emit agentkit.OutboundEmit, req permission.Request) error {
	agentID := session.AgentIDFromContext(ctx)
	platformID := session.PlatformFromContext(ctx)
	userID := session.UserIDFromContext(ctx)
	return emit(ctx, agentkit.OutboundEvent{
		Route:      session.RouteRefFromContext(ctx),
		AgentID:    agentID,
		PlatformID: platformID,
		UserID:     userID,
		Type:       agentkit.EventPermissionRequest,
		Data: agentkit.MarshalOutboundData(permission.RequestPayload{
			Request: req,
		}),
	})
}

func (c *Control) emitPermissionResolved(ctx context.Context, emit agentkit.OutboundEmit, id string, result permission.Result) error {
	agentID := session.AgentIDFromContext(ctx)
	platformID := session.PlatformFromContext(ctx)
	userID := session.UserIDFromContext(ctx)
	resolved := result
	resolved.ID = id
	return emit(ctx, agentkit.OutboundEvent{
		Route:      session.RouteRefFromContext(ctx),
		AgentID:    agentID,
		PlatformID: platformID,
		UserID:     userID,
		Type:       agentkit.EventPermissionResolved,
		Data:       agentkit.MarshalOutboundData(resolved),
	})
}

func newPermissionRequestID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "perm"
	}
	return hex.EncodeToString(b[:])
}

var _ permission.Broker = (*Control)(nil)
