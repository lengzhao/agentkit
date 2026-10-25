package multiplex

import (
	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/chathistory"
)

var _ chathistory.Router = (*Platform)(nil)

func (m *Platform) ChatHistoryFor(route agentkit.RouteRef) chathistory.Provider {
	p, ok := m.platforms[route.DestinationID()]
	if !ok {
		return nil
	}
	if provider, ok := p.(chathistory.Provider); ok {
		return provider
	}
	return nil
}
