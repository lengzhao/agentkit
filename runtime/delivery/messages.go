package delivery

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/lengzhao/agentkit"
	cap "github.com/lengzhao/agentkit/cap/delivery"
	"github.com/lengzhao/agentkit/runtime/rctx"
)

type MessagesConfig struct {
	// Instances explicitly names leaves in Platforms order. Required when multiple
	// leaves share a protocol and have not already been assigned stable identities.
	Instances []string `json:"instances,omitempty"`
}
type MessagesDeps struct {
	Platforms []agentkit.Platform `json:"platforms"`
	Admission cap.Admission       `json:"admission,omitempty"`
}
type messageBinding struct {
	platform agentkit.Platform
	protocol string
}
type messages struct {
	leaves    map[string]messageBinding
	admission cap.Admission
}

// NewMessages registers delivery/messages. Wire only leaf platforms, not a
// containing multiplex platform, so background plugins cannot create DI cycles.
func NewMessages(cfg MessagesConfig, deps MessagesDeps) (cap.Messages, error) {
	if len(deps.Platforms) == 0 {
		return nil, fmt.Errorf("delivery/messages requires leaf platforms")
	}
	if len(cfg.Instances) != 0 && len(cfg.Instances) != len(deps.Platforms) {
		return nil, fmt.Errorf("delivery/messages instances must match platforms")
	}
	s := &messages{leaves: map[string]messageBinding{}, admission: deps.Admission}
	for i, p := range deps.Platforms {
		identity, ok := p.(agentkit.PlatformIdentifier)
		if !ok || identity.PlatformID() == "" {
			return nil, fmt.Errorf("delivery/messages leaf %d requires a protocol identity", i)
		}
		protocol := identity.PlatformID()
		instance := protocol
		if x, ok := p.(interface{ PlatformInstance() string }); ok && x.PlatformInstance() != "" {
			instance = x.PlatformInstance()
		}
		if len(cfg.Instances) > 0 {
			instance = cfg.Instances[i]
			if instance == "" {
				return nil, fmt.Errorf("delivery/messages requires nonempty instance IDs")
			}
			if x, ok := p.(interface{ PlatformInstance() string }); ok && x.PlatformInstance() != "" && x.PlatformInstance() != instance {
				return nil, fmt.Errorf("delivery/messages instance identity conflict")
			}
		}
		if _, ok := s.leaves[instance]; ok {
			return nil, fmt.Errorf("delivery/messages duplicate instance %q", instance)
		}
		s.leaves[instance] = messageBinding{p, protocol}
	}
	// Assign only after validating the entire configuration.
	for instance, b := range s.leaves {
		if x, ok := b.platform.(agentkit.PlatformInstanceSetter); ok {
			x.SetPlatformInstance(instance)
		}
	}
	return s, nil
}

func (s *messages) target(ctx context.Context, route agentkit.RouteRef, call cap.Call) (agentkit.Platform, agentkit.RouteRef, error) {
	if err := ctx.Err(); err != nil {
		return nil, route, &cap.Error{Code: cap.CodeTransport, Outcome: cap.OutcomeNotSent, Cause: err}
	}
	// Instance takes precedence; never guess a leaf from a protocol in a multi-instance setup.
	b, ok := s.leaves[route.DestinationID()]
	if !ok {
		return nil, route, &cap.Error{Code: cap.CodeInvalidTarget, Outcome: cap.OutcomeNotSent}
	}
	if x, ok := b.platform.(interface{ PlatformInstance() string }); ok && x.PlatformInstance() != route.DestinationID() {
		return nil, route, &cap.Error{Code: cap.CodeInvalidTarget, Outcome: cap.OutcomeNotSent}
	}
	normalized, err := rctx.RouteForPlatform(route, b.protocol)
	if err != nil {
		return nil, route, &cap.Error{Code: cap.CodeInvalidTarget, Outcome: cap.OutcomeNotSent, Cause: err}
	}
	normalized.Instance = route.DestinationID()
	target, ok := rctx.DecodeSessionRoute(normalized)
	if !ok || target.ChannelID == "" {
		return nil, route, &cap.Error{Code: cap.CodeInvalidTarget, Outcome: cap.OutcomeNotSent}
	}
	// ReplyTo is an ephemeral turn anchor, never a persistent business target.
	normalized = rctx.BuildSessionRoute(agentkit.SessionRouteInput{Platform: b.protocol, Instance: route.DestinationID(), ChannelID: target.ChannelID, ThreadID: target.ThreadID})
	call.Route = normalized
	if s.admission != nil {
		if err := s.admission.AuthorizeDelivery(ctx, call); err != nil {
			return nil, normalized, &cap.Error{Code: cap.CodeForbidden, Outcome: cap.OutcomeNotSent, Cause: err}
		}
	}
	return b.platform, normalized, nil
}
func unsupported() error { return &cap.Error{Code: cap.CodeUnsupported, Outcome: cap.OutcomeNotSent} }
func finish(ctx context.Context, call cap.Call, err error) {
	code := ""
	var e *cap.Error
	if errors.As(err, &e) {
		code = string(e.Code)
	} else if err != nil {
		code = string(cap.CodeFailed)
	}
	// Do not log routes, content, SDK response text, user IDs or secrets.
	slog.DebugContext(ctx, "delivery operation", "operation", call.Operation, "caller", call.Caller, "operation_id", call.OperationID, "error_code", code)
}
func (s *messages) publish(ctx context.Context, req cap.PublishRequest, topic bool) (receipt cap.Receipt, err error) {
	call := cap.Call{Caller: req.Caller, OperationID: req.OperationID, Operation: "publish"}
	if topic {
		call.Operation = "create_topic"
	}
	defer func() { finish(ctx, call, err) }()
	receipt = cap.Receipt{Route: req.Route, Outcome: cap.OutcomeNotSent, Deduplication: cap.Deduplication{Mode: "none"}}
	p, route, err := s.target(ctx, req.Route, call)
	if err != nil {
		return receipt, err
	}
	req.Route = route
	receipt.Route = route
	if topic {
		if c, ok := p.(cap.TopicCreator); ok {
			return c.CreateTopic(ctx, req)
		}
	} else {
		if c, ok := p.(cap.Publisher); ok {
			return c.Publish(ctx, req)
		}
	}
	return receipt, unsupported()
}
func (s *messages) Publish(ctx context.Context, req cap.PublishRequest) (cap.Receipt, error) {
	return s.publish(ctx, req, false)
}
func (s *messages) CreateTopic(ctx context.Context, req cap.PublishRequest) (cap.Receipt, error) {
	return s.publish(ctx, req, true)
}
func (s *messages) EditMessage(ctx context.Context, req cap.EditRequest) (err error) {
	call := cap.Call{Caller: req.Caller, OperationID: req.OperationID, Operation: "edit"}
	defer func() { finish(ctx, call, err) }()
	if req.Message.ID == "" {
		return &cap.Error{Code: cap.CodeInvalidTarget, Outcome: cap.OutcomeNotSent}
	}
	p, route, err := s.target(ctx, req.Message.Route, call)
	if err != nil {
		return err
	}
	req.Message.Route = route
	if c, ok := p.(cap.Editor); ok {
		return c.EditMessage(ctx, req)
	}
	return unsupported()
}
func (s *messages) ReadMessage(ctx context.Context, req cap.ReadRequest) (msg cap.Message, err error) {
	ref := req.Message
	call := cap.Call{Operation: "read", Caller: req.Caller, OperationID: req.OperationID}
	defer func() { finish(ctx, call, err) }()
	if ref.ID == "" {
		return msg, &cap.Error{Code: cap.CodeInvalidTarget, Outcome: cap.OutcomeNotSent}
	}
	p, route, err := s.target(ctx, ref.Route, call)
	if err != nil {
		return msg, err
	}
	ref.Route = route
	if c, ok := p.(cap.Reader); ok {
		req.Message = ref
		return c.ReadMessage(ctx, req)
	}
	return msg, unsupported()
}
