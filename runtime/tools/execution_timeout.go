package tools

import (
	"encoding/json"
	"time"

	"github.com/lengzhao/agentkit"
	capsubagent "github.com/lengzhao/agentkit/cap/subagent"
)

const delegateExposedName = "delegate"

type callTimeoutInput struct {
	TimeoutSeconds *int `json:"timeoutSeconds"`
}

// executionTimeout resolves the execution bound for one call: the configured
// timeout, unless the call declares a larger per-call timeout — delegate's
// timeoutSeconds, or a tool implementing agentkit.CallTimeoutProvider (e.g.
// workflow tools, whose default is the summed step timeouts plus a buffer).
func (r *Runtime) executionTimeout(call agentkit.ToolCall, tool agentkit.Tool) time.Duration {
	configured := r.timeoutFor(call.Name)
	perCall := perCallTimeout(call, tool)
	if perCall <= 0 {
		return configured
	}
	if configured <= 0 || perCall > configured {
		return perCall
	}
	return configured
}

func perCallTimeout(call agentkit.ToolCall, tool agentkit.Tool) time.Duration {
	if isDelegateTool(call.Name) {
		if d := timeoutSecondsFromInput(call.Input); d > 0 {
			return d
		}
		return time.Duration(capsubagent.DefaultDelegationTimeoutSeconds) * time.Second
	}
	if tp, ok := tool.(agentkit.CallTimeoutProvider); ok {
		return tp.CallTimeout(call.Input)
	}
	return 0
}

func isDelegateTool(name string) bool {
	return name == delegateExposedName || ExposedToolName(name) == delegateExposedName
}

func timeoutSecondsFromInput(raw json.RawMessage) time.Duration {
	if len(raw) == 0 {
		return 0
	}
	var input callTimeoutInput
	if err := json.Unmarshal(raw, &input); err != nil {
		return 0
	}
	if input.TimeoutSeconds == nil || *input.TimeoutSeconds <= 0 {
		return 0
	}
	return time.Duration(*input.TimeoutSeconds) * time.Second
}
