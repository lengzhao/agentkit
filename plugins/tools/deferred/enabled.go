package deferred

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/lengzhao/agentkit"
)

// EnabledSetting controls when deferred bridges replace deferrable tools.
type EnabledSetting string

const (
	EnabledOff  EnabledSetting = "off"
	EnabledOn   EnabledSetting = "on"
	EnabledAuto EnabledSetting = "auto"
)

func (e *EnabledSetting) UnmarshalJSON(data []byte) error {
	if len(data) == 0 {
		*e = EnabledAuto
		return nil
	}
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		*e = normalizeEnabledSetting(s)
		return nil
	}
	var b bool
	if err := json.Unmarshal(data, &b); err == nil {
		*e = EnabledOn
		if !b {
			*e = EnabledOff
		}
		return nil
	}
	return fmt.Errorf("enabled must be auto, on, off, or a boolean")
}

func normalizeEnabledSetting(raw string) EnabledSetting {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "auto":
		return EnabledAuto
	case "on", "true", "1", "yes":
		return EnabledOn
	case "off", "false", "0", "no":
		return EnabledOff
	default:
		return EnabledAuto
	}
}

func (e EnabledSetting) normalize() EnabledSetting {
	if e == "" {
		return EnabledAuto
	}
	return normalizeEnabledSetting(string(e))
}

func (e EnabledSetting) active(deferrable []agentkit.ToolSpec, thresholdPct float64, contextLength int) bool {
	switch e.normalize() {
	case EnabledOff:
		return false
	case EnabledOn:
		return len(deferrable) > 0
	case EnabledAuto:
		if len(deferrable) == 0 {
			return false
		}
		est := estimateDeferrableTokens(deferrable)
		ctx := contextLength
		if ctx <= 0 {
			ctx = defaultContextWindow
		}
		threshold := int(float64(ctx) * (thresholdPct / 100.0))
		if threshold < 1 {
			threshold = 1
		}
		return est >= threshold
	default:
		return false
	}
}
