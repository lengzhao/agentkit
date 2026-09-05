package chathistory

import (
	"github.com/lengzhao/agentkit/cap/chathistory"
)

// RouterFromPlatform adapts a Platform (or existing Router) for chat-history tools.
func RouterFromPlatform(platform any) chathistory.Router {
	if platform == nil {
		return nil
	}
	if router, ok := platform.(chathistory.Router); ok {
		return router
	}
	if provider, ok := platform.(chathistory.Provider); ok {
		return &singlePlatformRouter{provider: provider}
	}
	return noopRouter{}
}

type singlePlatformRouter struct {
	provider chathistory.Provider
}

func (r *singlePlatformRouter) ChatHistoryFor(_ string) chathistory.Provider {
	return r.provider
}

type noopRouter struct{}

func (noopRouter) ChatHistoryFor(string) chathistory.Provider { return nil }
