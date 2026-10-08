package tools

import (
	"encoding/json"
	"time"

	"github.com/lengzhao/agentkit"
	capsubagent "github.com/lengzhao/agentkit/cap/subagent"
)

const delegateExposedName = "delegate"

type delegateCallTimeout struct {
	TimeoutSeconds *int `json:"timeoutSeconds"`
}

func (r *Runtime) executionTimeout(call agentkit.ToolCall) time.Duration {
	configured := r.timeoutFor(call.Name)
	if !isDelegateTool(call.Name) {
		return configured
	}
	perCall := delegateTimeoutFromInput(call.Input)
	if perCall <= 0 {
		perCall = time.Duration(capsubagent.DefaultDelegationTimeoutSeconds) * time.Second
	}
	if configured <= 0 || perCall > configured {
		return perCall
	}
	return configured
}

func isDelegateTool(name string) bool {
	return name == delegateExposedName || ExposedToolName(name) == delegateExposedName
}

func delegateTimeoutFromInput(raw json.RawMessage) time.Duration {
	if len(raw) == 0 {
		return 0
	}
	var input delegateCallTimeout
	if err := json.Unmarshal(raw, &input); err != nil {
		return 0
	}
	if input.TimeoutSeconds == nil || *input.TimeoutSeconds <= 0 {
		return 0
	}
	return time.Duration(*input.TimeoutSeconds) * time.Second
}
