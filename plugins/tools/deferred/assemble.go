package deferred

import (
	"fmt"
	"strings"

	"github.com/lengzhao/agentkit"
)

func assembleVisible(eager []agentkit.ToolSpec, deferrable []agentkit.ToolSpec, cfg DisclosureConfig, contextLength int) []agentkit.ToolSpec {
	if len(deferrable) == 0 {
		return append([]agentkit.ToolSpec(nil), eager...)
	}
	catalog := buildCatalog(deferrable)
	listing := ""
	if cfg.Listing != "off" {
		budget := cfg.listingBudgetChars(contextLength)
		if cfg.Listing == "on" || budget > 0 {
			listing = listingText(catalog, budget)
			if cfg.Listing == "auto" && listing == "" && len(deferrable) > 0 {
				listing = listingText(catalog, budget)
			}
		}
	}
	bridges := bridgeSpecs(len(deferrable), listing)
	out := make([]agentkit.ToolSpec, 0, len(eager)+len(bridges))
	out = append(out, eager...)
	out = append(out, bridges...)
	return out
}

func bridgeSpecs(deferredCount int, listing string) []agentkit.ToolSpec {
	searchDesc := fmt.Sprintf(
		"Search %d additional tools loaded on demand. Returns matching tool names per query plus a shared tools map with short descriptions. Follow with %s for full parameter schemas, then %s to invoke. Tools listed in the system prompt are already available.",
		deferredCount, ToolDescribe, ToolCall,
	)
	if listing != "" {
		searchDesc += "\n\nDeferred capabilities (load with " + ToolDescribe + "):\n" + listing
	}
	return []agentkit.ToolSpec{
		{
			Name:        ToolSearch,
			Description: searchDesc,
			InputSchema: agentkit.JSONSchema{
				Type: "object",
				Properties: map[string]agentkit.JSONSchema{
					"queries": {
						Type:        "array",
						Description: "Search queries; a single string is accepted as one query.",
						Items:       &agentkit.JSONSchema{Type: "string"},
					},
					"limit": {
						Type:        "integer",
						Description: "Max matches per query (default from config).",
					},
				},
				Required: []string{"queries"},
			},
		},
		{
			Name: ToolDescribe,
			Description: fmt.Sprintf(
				"Load full JSON schemas for tools returned by %s. Required before %s when parameters are unknown.",
				ToolSearch, ToolCall,
			),
			InputSchema: agentkit.JSONSchema{
				Type: "object",
				Properties: map[string]agentkit.JSONSchema{
					"names": {
						Type:        "array",
						Description: "Exact tool names from tool_search.",
						Items:       &agentkit.JSONSchema{Type: "string"},
					},
				},
				Required: []string{"names"},
			},
		},
		{
			Name: ToolCall,
			Description: fmt.Sprintf(
				"Invoke deferred tools. Pass calls as an array of {name, arguments}. Argument shapes match each tool schema (see %s). Policy and hooks apply to the underlying tool.",
				ToolDescribe,
			),
			InputSchema: agentkit.JSONSchema{
				Type: "object",
				Properties: map[string]agentkit.JSONSchema{
					"calls": {
						Type:        "array",
						Description: "One entry per invocation.",
						Items: &agentkit.JSONSchema{
							Type: "object",
							Properties: map[string]agentkit.JSONSchema{
								"name":      {Type: "string"},
								"arguments": {Type: "object"},
							},
							Required: []string{"name", "arguments"},
						},
					},
				},
				Required: []string{"calls"},
			},
		},
	}
}

func toolSummaryMap(specs []agentkit.ToolSpec) map[string]map[string]string {
	out := make(map[string]map[string]string, len(specs))
	for _, spec := range specs {
		required := requiredParamNames(spec.InputSchema)
		out[spec.Name] = map[string]string{
			"description": clip(spec.Description, 500),
			"required":    strings.Join(required, ", "),
		}
	}
	return out
}

func requiredParamNames(schema agentkit.JSONSchema) []string {
	if len(schema.Required) > 0 {
		return schema.Required
	}
	return nil
}

func clip(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}
