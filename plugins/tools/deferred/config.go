package deferred

import (
	"fmt"
	"math"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/tools"
)

// Config matches tools/runtime config at the top level, plus disclosure fields.
// Swap use: tools/runtime → tools/deferred without changing deps or timeout/allowTools keys.
type Config struct {
	tools.RuntimeConfig
	DisclosureConfig
}

// DisclosureConfig controls progressive tool disclosure only.
type DisclosureConfig struct {
	Enabled            EnabledSetting `json:"enabled"`
	ThresholdPct       float64  `json:"thresholdPct"`
	ListingMaxTokens   int      `json:"listingMaxTokens"`
	SearchDefaultLimit int      `json:"searchDefaultLimit"`
	MaxSearchLimit     int      `json:"maxSearchLimit"`
	Listing            string   `json:"listing"` // auto | on | off
	EagerTools         []string `json:"eagerTools,omitempty"`
	DeferTools         []string `json:"deferTools,omitempty"`
}

func (c Config) Normalize() Config {
	out := c
	out.DisclosureConfig = c.DisclosureConfig.Normalize()
	return out
}

// SetDefaults implements pluginkit.Defaulter.
func (c *Config) SetDefaults() {
	c.DisclosureConfig.SetDefaults()
}

// Validate implements pluginkit.Validator.
func (c *Config) Validate() error {
	return c.DisclosureConfig.Validate()
}

// SetDefaults implements pluginkit.Defaulter.
func (c *DisclosureConfig) SetDefaults() {
	c.Enabled = c.Enabled.normalize()
	if c.ThresholdPct == 0 {
		c.ThresholdPct = 5
	}
	if c.ListingMaxTokens == 0 {
		c.ListingMaxTokens = 4000
	}
	if c.SearchDefaultLimit == 0 {
		c.SearchDefaultLimit = 5
	}
	if c.MaxSearchLimit == 0 {
		c.MaxSearchLimit = 25
	}
	if c.Listing == "" {
		c.Listing = "auto"
	}
}

// Validate implements pluginkit.Validator.
func (c *DisclosureConfig) Validate() error {
	if c.ThresholdPct <= 0 || c.ThresholdPct > 100 {
		return fmt.Errorf("tools/deferred thresholdPct must be in (0, 100], got %v", c.ThresholdPct)
	}
	if c.ListingMaxTokens <= 0 {
		return fmt.Errorf("tools/deferred listingMaxTokens must be positive")
	}
	if c.SearchDefaultLimit <= 0 || c.MaxSearchLimit <= 0 {
		return fmt.Errorf("tools/deferred searchDefaultLimit and maxSearchLimit must be positive")
	}
	if c.MaxSearchLimit > 50 {
		return fmt.Errorf("tools/deferred maxSearchLimit must be <= 50, got %d", c.MaxSearchLimit)
	}
	if c.SearchDefaultLimit > c.MaxSearchLimit {
		return fmt.Errorf("tools/deferred searchDefaultLimit (%d) exceeds maxSearchLimit (%d)", c.SearchDefaultLimit, c.MaxSearchLimit)
	}
	switch c.Listing {
	case "on", "off", "auto":
	default:
		return fmt.Errorf("tools/deferred listing must be auto, on, or off, got %q", c.Listing)
	}
	return nil
}

func (c DisclosureConfig) Normalize() DisclosureConfig {
	out := c
	out.Enabled = out.Enabled.normalize()
	if out.ThresholdPct <= 0 {
		out.ThresholdPct = 5
	}
	if out.ThresholdPct > 100 {
		out.ThresholdPct = 100
	}
	if out.ListingMaxTokens <= 0 {
		out.ListingMaxTokens = 4000
	}
	if out.SearchDefaultLimit <= 0 {
		out.SearchDefaultLimit = 5
	}
	if out.MaxSearchLimit <= 0 {
		out.MaxSearchLimit = 25
	}
	if out.MaxSearchLimit > 50 {
		out.MaxSearchLimit = 50
	}
	if out.SearchDefaultLimit > out.MaxSearchLimit {
		out.SearchDefaultLimit = out.MaxSearchLimit
	}
	switch out.Listing {
	case "on", "off", "auto":
	default:
		out.Listing = "auto"
	}
	return out
}

func (c DisclosureConfig) disclosureActive(deferrable []agentkit.ToolSpec, contextLength int) bool {
	return c.Enabled.active(deferrable, c.ThresholdPct, contextLength)
}

func (c DisclosureConfig) listingBudgetChars(contextLength int) int {
	pctLeg := 10_000
	if contextLength > 0 {
		pctLeg = int(math.Round(float64(contextLength) * (c.ThresholdPct / 100.0) / charsPerToken))
	}
	maxLeg := c.ListingMaxTokens
	if maxLeg > 60000 {
		maxLeg = 60000
	}
	if maxLeg < 200 {
		maxLeg = 200
	}
	budget := min(pctLeg, maxLeg)
	if budget < 0 {
		return 0
	}
	return budget * charsPerToken
}
