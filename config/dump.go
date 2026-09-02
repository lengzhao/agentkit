package config

import "gopkg.in/yaml.v3"

// DumpOptions controls resolved config output.
type DumpOptions struct {
	// Redact scrubs interpolated secrets while keeping credential refs (env:, file:).
	Redact bool
}

// DumpResolvedYAML loads and renders the effective config graph (merge, extends,
// prune, interpolate) and returns YAML bytes suitable for inspection.
func DumpResolvedYAML(basePath string, overlayPaths []string, opts DumpOptions) ([]byte, error) {
	resolved, err := ResolveFiles(basePath, overlayPaths...)
	if err != nil {
		return nil, err
	}
	if !opts.Redact {
		return resolved, nil
	}
	raw, err := parseInstanceMap(resolved)
	if err != nil {
		return nil, err
	}
	RedactInstances(raw)
	return yaml.Marshal(raw)
}
