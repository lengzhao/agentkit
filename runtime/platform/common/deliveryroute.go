package common

import (
	"github.com/lengzhao/agentkit/cap/delivery"
	rtdelivery "github.com/lengzhao/agentkit/runtime/delivery"
)

type (
	DeliveryRoute      = delivery.Route
	DeliveryRouteInput = delivery.RouteInput
)

var (
	ResolveDeliveryRoute       = rtdelivery.ResolveRoute
	NormalizeDeliverySessionID = rtdelivery.NormalizeSessionID
	IsSlackChannelID           = rtdelivery.IsSlackChannelID
)
