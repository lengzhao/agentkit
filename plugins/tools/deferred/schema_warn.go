package deferred

import (
	"encoding/json"
	"log/slog"

	"github.com/lengzhao/agentkit"
)

// minDescriptionLenForHollowSchemaWarn flags tools whose prose description is
// long enough that the model may infer parameters from text when JSON Schema
// properties are missing (typical AgentHub catalog sync gap).
const minDescriptionLenForHollowSchemaWarn = 120

func schemaHasProperties(schema agentkit.JSONSchema) bool {
	if len(schema.Properties) > 0 {
		return true
	}
	if len(schema.Raw) == 0 {
		return false
	}
	props, ok := schema.Raw["properties"].(map[string]any)
	return ok && len(props) > 0
}

func schemaJSONBytes(schema agentkit.JSONSchema) int {
	raw, err := json.Marshal(schema)
	if err != nil {
		return 0
	}
	return len(raw)
}

// warnHollowDescribeSchema logs when tool_describe returns a tool whose
// InputSchema has no properties while the description is substantial.
func warnHollowDescribeSchema(spec agentkit.ToolSpec) {
	if schemaHasProperties(spec.InputSchema) {
		return
	}
	if len(spec.Description) < minDescriptionLenForHollowSchemaWarn {
		return
	}
	slog.Warn("tools/deferred: tool_describe loaded hollow input schema (no properties); model may omit arguments — check upstream catalog/MCP sync",
		"tool", spec.Name,
		"description_len", len(spec.Description),
		"input_schema_bytes", schemaJSONBytes(spec.InputSchema),
	)
}
