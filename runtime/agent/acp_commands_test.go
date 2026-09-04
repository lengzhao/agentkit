package agent

import (
	"strings"
	"testing"

	"github.com/lengzhao/agentkit"
)

func TestParseACPArgs(t *testing.T) {
	t.Parallel()

	cases := []struct {
		args        string
		agentID     string
		configKey   string
		configValue string
		mode        acpParseMode
		wantErr     bool
	}{
		{args: "", mode: acpParseList},
		{args: "claude", agentID: "claude", mode: acpParseShow},
		{args: "claude config", agentID: "claude", mode: acpParseConfigList},
		{args: "claude config model claude-sonnet-5", agentID: "claude", configKey: "model", configValue: "claude-sonnet-5", mode: acpParseConfigSet},
		{args: "claude /model claude-sonnet-5", wantErr: true},
	}

	for _, tc := range cases {
		agentID, configKey, configValue, mode, err := parseACPArgs(tc.args)
		if tc.wantErr {
			if err == nil {
				t.Fatalf("parseACPArgs(%q) expected error", tc.args)
			}
			continue
		}
		if err != nil {
			t.Fatalf("parseACPArgs(%q) err = %v", tc.args, err)
		}
		if agentID != tc.agentID || configKey != tc.configKey || configValue != tc.configValue || mode != tc.mode {
			t.Fatalf("parseACPArgs(%q) = (%q, %q, %q, %v), want (%q, %q, %q, %v)",
				tc.args, agentID, configKey, configValue, mode, tc.agentID, tc.configKey, tc.configValue, tc.mode)
		}
	}
}

func TestFormatACPConfigList(t *testing.T) {
	t.Parallel()

	got := formatACPConfigList("claude", []agentkit.ACPConfigOptionInfo{
		{
			ID:           "model",
			Name:         "Model",
			Category:     "model",
			CurrentValue: "claude-opus-5",
			Options: []agentkit.ACPConfigOptionValue{
				{Value: "claude-sonnet-5", Name: "Sonnet 5"},
			},
		},
	})
	for _, want := range []string{
		"agent: claude config options",
		"Model (model) [model] = claude-opus-5",
		"claude-sonnet-5",
		"/acp claude config <key> <value>",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("formatACPConfigList() missing %q:\n%s", want, got)
		}
	}
}
