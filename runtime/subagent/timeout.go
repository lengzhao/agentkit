package subagent

import (
	"fmt"
	"time"

	capsubagent "github.com/lengzhao/agentkit/cap/subagent"
)

// DelegationWallClock returns the wall-clock bound for one delegation.
// Per-call timeoutSeconds on the request wins when set and positive; otherwise configured applies.
func DelegationWallClock(req capsubagent.Request, configured time.Duration) (time.Duration, error) {
	if req.TimeoutSeconds == nil {
		return configured, nil
	}
	secs := *req.TimeoutSeconds
	if secs < 0 {
		return 0, fmt.Errorf("timeoutSeconds must not be negative")
	}
	if secs == 0 {
		return configured, nil
	}
	return time.Duration(secs) * time.Second, nil
}
