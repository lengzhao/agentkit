package runner

import "github.com/lengzhao/agentkit"

func (r *Root) BuildInboundPromptPrefixForTest(event agentkit.MessageEvent, deliveryID agentkit.SessionID) string {
	return r.buildInboundPromptPrefix(event, deliveryID)
}

func (r *Root) FormatInboundEventForTest(event agentkit.MessageEvent, deliveryID agentkit.SessionID) agentkit.MessageEvent {
	return r.formatInboundEvent(event, deliveryID)
}
