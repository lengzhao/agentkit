package acpremote

import (
	"testing"

	acp "github.com/coder/acp-go-sdk"
)

func TestSessionStateCatalog(t *testing.T) {
	t.Parallel()

	state := &sessionState{}
	modelCategory := acp.SessionConfigOptionCategoryModel
	state.applyNewSession(acp.NewSessionResponse{
		SessionId: "sess_1",
		ConfigOptions: []acp.SessionConfigOption{
			{
				Select: &acp.SessionConfigOptionSelect{
					Id:           "model",
					Name:         "Model",
					Category:     &modelCategory,
					CurrentValue: "claude-opus-5",
					Type:         "select",
				},
			},
		},
	})
	state.applyUpdate(acp.SessionUpdate{
		AvailableCommandsUpdate: &acp.SessionAvailableCommandsUpdate{
			AvailableCommands: []acp.AvailableCommand{
				{Name: "model", Description: "Switch model"},
			},
		},
	})

	catalog := state.catalog()
	if len(catalog.AvailableCommands) != 1 || catalog.AvailableCommands[0].Name != "model" {
		t.Fatalf("commands: %+v", catalog.AvailableCommands)
	}
	if len(catalog.ConfigOptions) != 1 || catalog.ConfigOptions[0].CurrentValue != "claude-opus-5" {
		t.Fatalf("config: %+v", catalog.ConfigOptions)
	}
}

func TestFindConfigOption(t *testing.T) {
	t.Parallel()

	state := &sessionState{}
	modelCategory := acp.SessionConfigOptionCategoryModel
	state.applyNewSession(acp.NewSessionResponse{
		SessionId: "sess_1",
		ConfigOptions: []acp.SessionConfigOption{
			{
				Select: &acp.SessionConfigOptionSelect{
					Id:           "model",
					Name:         "Model",
					Category:     &modelCategory,
					CurrentValue: "claude-opus-5",
					Type:         "select",
					Options: acp.SessionConfigSelectOptions{
						Ungrouped: &acp.SessionConfigSelectOptionsUngrouped{
							{Value: "claude-sonnet-5", Name: "Sonnet 5"},
						},
					},
				},
			},
		},
	})

	ref, ok := state.findConfigOption("model")
	if !ok || string(ref.id) != "model" {
		t.Fatalf("find by id: %+v ok=%v", ref, ok)
	}
	if _, ok := ref.options["claude-sonnet-5"]; !ok {
		t.Fatalf("options: %+v", ref.options)
	}
}
