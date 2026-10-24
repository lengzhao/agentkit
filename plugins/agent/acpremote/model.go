package acpremote

import (
	"strings"

	acp "github.com/coder/acp-go-sdk"
)

// modelFromConfig uses the semantic category, with the conventional option id
// as a fallback for agents that omit the optional category.
func modelFromConfig(options []acp.SessionConfigOption) string {
	if option := modelConfigOption(options); option != nil {
		return strings.TrimSpace(string(option.CurrentValue))
	}
	return ""
}

func modelConfigOption(options []acp.SessionConfigOption) *acp.SessionConfigOptionSelect {
	for _, option := range options {
		s := option.Select
		if s == nil {
			continue
		}
		if (s.Category != nil && *s.Category == acp.SessionConfigOptionCategoryModel) ||
			(s.Category == nil && s.Id == "model") {
			return s
		}
	}
	return nil
}
