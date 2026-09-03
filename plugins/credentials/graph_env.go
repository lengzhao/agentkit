package credentials

import (
	"strings"

	"github.com/lengzhao/agentkit/config"
)

const credentialsEnvUse = "credentials/env"

// EnvGraphSource exposes credentials/env config.env entries to ${env:NAME} / ${var:NAME}
// gate probes and interpolation. It does not read config.files or build a Store.
func EnvGraphSource(raw map[string]any) (config.EnvLookup, error) {
	values := make(map[string]string)
	for _, node := range raw {
		nodeMap, ok := node.(map[string]any)
		if !ok || instanceUse(nodeMap) != credentialsEnvUse {
			continue
		}
		configMap, _ := nodeMap["config"].(map[string]any)
		envMap, _ := configMap["env"].(map[string]any)
		for key, value := range envMap {
			if s, ok := value.(string); ok && strings.TrimSpace(s) != "" {
				values[key] = s
			}
		}
	}
	return config.MapEnvLookup(values), nil
}

func instanceUse(node map[string]any) string {
	use, _ := node["use"].(string)
	return use
}
