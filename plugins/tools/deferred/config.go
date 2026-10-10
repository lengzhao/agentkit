package deferred

import (
	"fmt"
	"math"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/tools"
)

// defaultSearchInlineSchemaMax is how many top search hits get their full
// schema inlined into the tool_search result when searchInlineSchemaMax is unset.
const defaultSearchInlineSchemaMax = 5

// searchInlineSchemaBudget caps total inlined schema bytes so tool_search
// results stay within a typical maxResultBytes envelope.
const searchInlineSchemaBudget = 8192

// Config matches tools/runtime config at the top level, plus disclosure fields.
// Swap use: tools/runtime → tools/deferred without changing deps or timeout/allowTools keys.
type Config struct {
	tools.RuntimeConfig
	DisclosureConfig
}

// DisclosureConfig controls progressive tool disclosure only.
type DisclosureConfig struct {
	Enabled            EnabledSetting `json:"enabled"`
	ThresholdPct       float64        `json:"thresholdPct"`
	ListingMaxTokens   int            `json:"listingMaxTokens"`
	SearchDefaultLimit int            `json:"searchDefaultLimit"`
	MaxSearchLimit     int            `json:"maxSearchLimit"`
	Listing            string         `json:"listing"` // auto | on | off
	// SearchInlineSchemaMax inlines full input schemas for the top-N search hits
	// in the tool_search result, so the model can invoke immediately without
	// tool_describe. 0 = default (5); negative = disable inlining.
	SearchInlineSchemaMax int `json:"searchInlineSchemaMax"`
	// CallBridge controls the tool_call bridge: on | off (default on). With
	// reveal + inline schemas, searched tools are directly callable by name, so
	// off drops the redundant bridge from the model's tool list.
	CallBridge string   `json:"callBridge"`
	EagerTools []string `json:"eagerTools,omitempty"`
	DeferTools []string `json:"deferTools,omitempty"`
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
	if c.SearchInlineSchemaMax == 0 {
		c.SearchInlineSchemaMax = defaultSearchInlineSchemaMax
	}
	if c.Listing == "" {
		c.Listing = "auto"
	}
	if c.CallBridge == "" {
		c.CallBridge = "on"
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
	switch c.CallBridge {
	case "", "on", "off":
	default:
		return fmt.Errorf("tools/deferred callBridge must be on or off, got %q", c.CallBridge)
	}
	return nil
}

// callBridgeEnabled reports whether the tool_call bridge is exposed.
func (c DisclosureConfig) callBridgeEnabled() bool {
	return c.CallBridge != "off"
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
	if out.SearchInlineSchemaMax == 0 {
		out.SearchInlineSchemaMax = defaultSearchInlineSchemaMax
	}
	if out.SearchInlineSchemaMax > 50 {
		out.SearchInlineSchemaMax = 50
	}
	if out.SearchDefaultLimit > out.MaxSearchLimit {
		out.SearchDefaultLimit = out.MaxSearchLimit
	}
	if out.CallBridge == "" {
		out.CallBridge = "on"
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
