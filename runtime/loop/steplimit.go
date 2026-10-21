package loop

import (
	"errors"
	"fmt"

	"github.com/lengzhao/agentkit"
)

// Step-limit detection and the user notice are duplicated here on purpose:
// the agent runtime raises an unexported "step-limit" error (its message is
// exactly the agentkit.StopStepLimit value) and loop/default only needs to
// swallow it. Importing runtime/agent for one error check would pull llm/* and
// compaction/* into every loop binary.

func isStepLimitError(err error) bool {
	for err != nil {
		if err.Error() == string(agentkit.StopStepLimit) {
			return true
		}
		err = errors.Unwrap(err)
	}
	return false
}

func stepLimitUserMessage(cap int) string {
	if cap <= 0 {
		return "已达到本回合模型步数上限，请发送新消息继续。"
	}
	return fmt.Sprintf("已达到本回合模型步数上限（%d 步），请发送新消息继续。", cap)
}
