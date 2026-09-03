package multiplex

import "github.com/lengzhao/agentkit/cap/chathistory"

var _ chathistory.Router = (*Platform)(nil)

func (m *Platform) ChatHistoryFor(id string) chathistory.Provider {
	p, ok := m.platforms[id]
	if !ok {
		return nil
	}
	if provider, ok := p.(chathistory.Provider); ok {
		return provider
	}
	return nil
}
