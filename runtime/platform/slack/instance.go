package slack

import "github.com/lengzhao/agentkit"

func (p *Platform) inboundRoute(d delivery, user string) agentkit.SessionRouteInput {
	route := d.inboundRoute(user)
	route.Instance = p.PlatformInstance()
	return route
}
