package settings

import "github.com/lengzhao/agentkit/plugindoc"

func init() {
	plugindoc.Register("settings/file", plugindoc.Doc{
		Summary: "Load persistent settings from a YAML or JSON file.",
		ConfigNotes: map[string]string{
			"path": "settings file, resolved through the workspace",
		},
	})
}
