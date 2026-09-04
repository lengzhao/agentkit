package subagent

import capschedule "github.com/lengzhao/agentkit/cap/schedule"

// SubmitBinder receives the runner inbound submit function at process start so
// async subagent completions can queue a follow-up turn without depending on
// loop in the plugin graph.
type SubmitBinder interface {
	BindSubmit(capschedule.SubmitFunc)
}
