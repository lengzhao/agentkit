package loop

import "github.com/lengzhao/agentkit/plugindoc"

func init() {
	plugindoc.Register("loop/default", plugindoc.Doc{
		Summary: "Route inbound messages to an agent and serialize turns per session.",
		ConfigNotes: map[string]string{
			"defaultAgent": "agent id used when the event names none; defaults to the single configured agent",
			"followUpMode": "how a message arriving mid-turn is handled: queue it or steer the running turn",
		},
	})
}
