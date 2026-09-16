package deferred

import (
	"encoding/json"
	"math"

	"github.com/lengzhao/agentkit"
)

// Default context when the loop does not pass window size (auto activation only).
const defaultContextWindow = 200_000

func estimateDeferrableTokens(specs []agentkit.ToolSpec) int {
	var chars int
	for _, spec := range specs {
		b, err := json.Marshal(spec)
		if err != nil {
			chars += len(spec.Name) + len(spec.Description) + 64
			continue
		}
		chars += len(b)
	}
	if chars == 0 {
		return 0
	}
	return int(math.Ceil(float64(chars) / float64(charsPerToken)))
}
