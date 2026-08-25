package skill

import "github.com/lengzhao/agentkit/plugindoc"

func init() {
	plugindoc.Register("skill/filesystem", plugindoc.Doc{
		Summary: "Scan directories for SKILL.md definitions.",
		ConfigNotes: map[string]string{
			"dirs": "directories to scan, in precedence order; each may use the global: or local: scope prefix",
		},
	})
}
