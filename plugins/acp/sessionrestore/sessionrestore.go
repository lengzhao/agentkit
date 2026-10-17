package sessionrestore

import (
	"fmt"

	capacp "github.com/lengzhao/agentkit/cap/acp"
	"github.com/lengzhao/agentkit/runtime/acpclient"
	"github.com/lengzhao/pluginkit"
)

// Config selects the ACP session re-attach strategy for agent/acp-remote.
type Config struct {
	// Strategy: resume | load | auto | skip | cursor (cursor = load for Cursor CLI).
	Strategy acpclient.RestoreStrategy `json:"strategy"`
}

func init() {
	pluginkit.Register("acp/session-restore", New)
}

// SetDefaults implements pluginkit.Defaulter.
func (c *Config) SetDefaults() {
	if c.Strategy == "" {
		c.Strategy = acpclient.StrategyAuto
	}
}

// Validate implements pluginkit.Validator.
func (c *Config) Validate() error {
	switch c.Strategy {
	case acpclient.StrategyResume, acpclient.StrategyLoad, acpclient.StrategyAuto,
		acpclient.StrategySkip, acpclient.StrategyCursor:
		return nil
	default:
		return fmt.Errorf("acp/session-restore: unknown strategy %q", c.Strategy)
	}
}

// New returns a SessionRestorer for deps.sessionRestore on agent/acp-remote.
func New(cfg Config) (capacp.SessionRestorer, error) {
	cfg.SetDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return acpclient.NewRestorer(cfg.Strategy), nil
}
