package deferred

import (
	"testing"

	"github.com/lengzhao/agentkit"
)

func TestSchemaHasProperties(t *testing.T) {
	t.Parallel()
	longDesc := "x"
	for len(longDesc) < minDescriptionLenForHollowSchemaWarn {
		longDesc += "x"
	}
	if schemaHasProperties(agentkit.ToolSpec{
		Name:        "t",
		Description: longDesc,
		InputSchema: agentkit.JSONSchema{Type: "object"},
	}.InputSchema) {
		t.Fatal("typed object without properties should be hollow")
	}
	raw := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"tenantId": map[string]any{"type": "string"},
		},
	}
	if !schemaHasProperties(agentkit.JSONSchema{Raw: raw}) {
		t.Fatal("Raw properties should count")
	}
	if !schemaHasProperties(agentkit.JSONSchema{
		Type: "object",
		Properties: map[string]agentkit.JSONSchema{
			"q": {Type: "string"},
		},
	}) {
		t.Fatal("structured Properties should count")
	}
}
