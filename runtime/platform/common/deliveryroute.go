package common

import (
	"github.com/lengzhao/agentkit/cap/delivery"
	"github.com/lengzhao/agentkit/runtime/rctx"
)

type (
	DeliveryRoute      = delivery.Route
	DeliveryRouteInput = delivery.RouteInput
)

// ResolveDeliveryRoute and NormalizeDeliverySessionID re-export the rctx
// helpers so platforms do not import runtime/delivery (which registers the
// delivery/assistant plugin).
var (
	ResolveDeliveryRoute       = rctx.ResolveRoute
	NormalizeDeliverySessionID = rctx.NormalizeSessionID
)
