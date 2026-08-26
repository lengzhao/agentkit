package agentkit

import (
	"encoding/json"
)

// MarshalJSON emits Raw when set so MCP and other external schemas round-trip intact.
func (s JSONSchema) MarshalJSON() ([]byte, error) {
	if len(s.Raw) > 0 {
		return json.Marshal(s.Raw)
	}
	type schema struct {
		Type        string                `json:"type,omitempty"`
		Description string                `json:"description,omitempty"`
		Properties  map[string]JSONSchema `json:"properties,omitempty"`
		Required    []string              `json:"required,omitempty"`
		Items       *JSONSchema           `json:"items,omitempty"`
	}
	return json.Marshal(schema{
		Type:        s.Type,
		Description: s.Description,
		Properties:  s.Properties,
		Required:    s.Required,
		Items:       s.Items,
	})
}

// UnmarshalJSON stores the full object in Raw and also fills known fields when present.
func (s *JSONSchema) UnmarshalJSON(data []byte) error {
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	s.Raw = raw
	type schema struct {
		Type        string                `json:"type,omitempty"`
		Description string                `json:"description,omitempty"`
		Properties  map[string]JSONSchema `json:"properties,omitempty"`
		Required    []string              `json:"required,omitempty"`
		Items       *JSONSchema           `json:"items,omitempty"`
	}
	var parsed schema
	if err := json.Unmarshal(data, &parsed); err != nil {
		return err
	}
	s.Type = parsed.Type
	s.Description = parsed.Description
	s.Properties = parsed.Properties
	s.Required = parsed.Required
	s.Items = parsed.Items
	return nil
}
