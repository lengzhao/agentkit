package agent

import (
	"errors"
	"fmt"
)

const defaultMaxSteps = 200

// resolveMaxSteps maps agent config to a per-turn step cap. Omitted maxSteps
// defaults to defaultMaxSteps; explicit 0 disables the cap.
func resolveMaxSteps(cfg *int) int {
	if cfg == nil {
		return defaultMaxSteps
	}
	if *cfg <= 0 {
		return 0
	}
	return *cfg
}

// StepLimitUserMessage is the user-visible notice when maxSteps is reached.
func StepLimitUserMessage(cap int) string {
	if cap <= 0 {
		return "已达到本回合模型步数上限，请发送新消息继续。"
	}
	return fmt.Sprintf("已达到本回合模型步数上限（%d 步），请发送新消息继续。", cap)
}

type stepLimitError struct {
	cap int
}

func (e *stepLimitError) Error() string {
	return "step-limit"
}

func newStepLimitError(cap int) error {
	return &stepLimitError{cap: cap}
}

// IsStepLimitError reports whether err ends a turn due to maxSteps.
func IsStepLimitError(err error) bool {
	_, ok := stepLimitFromError(err)
	return ok
}

func stepLimitFromError(err error) (int, bool) {
	var sle *stepLimitError
	if errors.As(err, &sle) {
		return sle.cap, true
	}
	return 0, false
}
