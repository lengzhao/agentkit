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
	bridges := bridgeSpecs(len(deferrable), listing, cfg.SearchInlineSchemaMax > 0, cfg.callBridgeEnabled())
	out := make([]agentkit.ToolSpec, 0, len(eager)+len(bridges))
	out = append(out, eager...)
	out = append(out, bridges...)
	return out
}

func bridgeSpecs(deferredCount int, listing string, inlineSchemas bool, callBridge bool) []agentkit.ToolSpec {
	// How the model should invoke a found tool: directly by name once revealed,
	// or via the tool_call bridge when it is exposed.
	invoke := "invoke them directly by name"
	if callBridge {
		invoke = fmt.Sprintf("invoke them directly by name (or via %s)", ToolCall)
	}
	var searchDesc string
	if inlineSchemas {
		searchDesc = fmt.Sprintf(
			"Search %d additional tools loaded on demand. Returns matching tool names per query; top matches include full parameter schemas and are immediately callable — %s, no %s needed. Use %s only for matches returned without a schema. Found tools stay available for the rest of this turn. Tools listed in the system prompt are already available.",
			deferredCount, invoke, ToolDescribe, ToolDescribe,
		)
	} else if callBridge {
		searchDesc = fmt.Sprintf(
			"Search %d additional tools loaded on demand. Returns matching tool names per query plus a shared tools map with short descriptions. Follow with %s for full parameter schemas, then %s to invoke. Tools listed in the system prompt are already available.",
			deferredCount, ToolDescribe, ToolCall,
		)
	} else {
		searchDesc = fmt.Sprintf(
			"Search %d additional tools loaded on demand. Returns matching tool names per query plus a shared tools map with short descriptions. Follow with %s for full parameter schemas, then invoke them directly by name. Tools listed in the system prompt are already available.",
			deferredCount, ToolDescribe,
		)
	}
	if listing != "" {
		searchDesc += "\n\nDeferred capabilities (load with " + ToolDescribe + "):\n" + listing
	}
	describeDesc := fmt.Sprintf(
		"Load full JSON schemas for %s matches that came back without an inlined schema. Skip when the search result already includes the schema. Required before %s when parameters are unknown.",
		ToolSearch, ToolCall,
	)
	if !callBridge {
		describeDesc = fmt.Sprintf(
			"Load full JSON schemas for %s matches that came back without an inlined schema. Skip when the search result already includes the schema. Required before invoking when parameters are unknown.",
			ToolSearch,
		)
	}
	specs := []agentkit.ToolSpec{
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
			Name:        ToolDescribe,
			Description: describeDesc,
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
	}
	if !callBridge {
		return specs
	}
	return append(specs, agentkit.ToolSpec{
		Name: ToolCall,
		Description: fmt.Sprintf(
			"Invoke deferred tools. Pass calls as an array of {name, arguments}. Argument shapes match each tool schema (inlined in %s results, or loaded via %s). Policy and hooks apply to the underlying tool.",
			ToolSearch, ToolDescribe,
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
	})
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
