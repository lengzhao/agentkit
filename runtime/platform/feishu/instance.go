package feishu

import "github.com/lengzhao/agentkit"

func (p *Platform) inboundRoute(msg inboundMessage) agentkit.SessionRouteInput {
	route := msg.inboundRoute(p.platformTag)
	route.Instance = p.PlatformInstance()
	return route
}
