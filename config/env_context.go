package config

import (
	"os"
	"strings"
)

// EnvLookup resolves a single variable name for ${env:NAME} / ${var:NAME} gate
// probes and interpolation during config loading.
type EnvLookup func(name string) (value string, ok bool)

// GraphEnvSource builds an EnvLookup from the merged instance graph after
// overlays and extends. It must not read secret files or instantiate plugins.
type GraphEnvSource func(raw map[string]any) (EnvLookup, error)

// EnvContext supplies values for ${env:VAR} and ${var:VAR} during prune and interpolation.
type EnvContext struct {
	lookup EnvLookup
}

// Lookup reports whether name is available for ${env:NAME} / ${var:NAME} gating.
func (c EnvContext) Lookup(name string) (string, bool) {
	if c.lookup == nil {
		return "", false
	}
	return c.lookup(name)
}

// ResolveOption configures env resolution during ResolveYAML.
type ResolveOption func(*resolveOptions)

type resolveOptions struct {
	lookups      []EnvLookup
	graphSources []GraphEnvSource
}

// WithEnvLookup appends a lookup tried after the process environment.
func WithEnvLookup(lookup EnvLookup) ResolveOption {
	return func(o *resolveOptions) {
		if lookup != nil {
			o.lookups = append(o.lookups, lookup)
		}
	}
}

// WithGraphEnvSource appends a graph-backed lookup tried after process env and
// any WithEnvLookup sources.
func WithGraphEnvSource(source GraphEnvSource) ResolveOption {
	return func(o *resolveOptions) {
		if source != nil {
			o.graphSources = append(o.graphSources, source)
		}
	}
}

var registeredGraphEnvSources []GraphEnvSource

// RegisterGraphEnvSource registers a graph env source used by ResolveYAML and
// ResolveFiles in addition to per-call WithGraphEnvSource options. Plugins such
// as credentials/env call this from init.
func RegisterGraphEnvSource(source GraphEnvSource) {
	if source != nil {
		registeredGraphEnvSources = append(registeredGraphEnvSources, source)
	}
}

func applyResolveOptions(opts []ResolveOption) resolveOptions {
	var out resolveOptions
	for _, opt := range opts {
		if opt != nil {
			opt(&out)
		}
	}
	return out
}

// BuildEnvContext constructs an EnvContext from the merged graph and options.
func BuildEnvContext(raw map[string]any, opts ...ResolveOption) (EnvContext, error) {
	return buildEnvContext(raw, opts)
}

func buildEnvContext(raw map[string]any, opts []ResolveOption) (EnvContext, error) {
	ro := applyResolveOptions(opts)
	lookups := make([]EnvLookup, 0, 2+len(ro.lookups)+len(registeredGraphEnvSources)+len(ro.graphSources))
	lookups = append(lookups, processEnvLookup)
	lookups = append(lookups, ro.lookups...)
	for _, source := range registeredGraphEnvSources {
		lookup, err := source(raw)
		if err != nil {
			return EnvContext{}, err
		}
		if lookup != nil {
			lookups = append(lookups, lookup)
		}
	}
	for _, source := range ro.graphSources {
		lookup, err := source(raw)
		if err != nil {
			return EnvContext{}, err
		}
		if lookup != nil {
			lookups = append(lookups, lookup)
		}
	}
	return EnvContext{lookup: chainEnvLookups(lookups)}, nil
}

func chainEnvLookups(lookups []EnvLookup) EnvLookup {
	return func(name string) (string, bool) {
		for _, lookup := range lookups {
			if lookup == nil {
				continue
			}
			if value, ok := lookup(name); ok {
				return value, true
			}
		}
		return "", false
	}
}

func processEnvLookup(name string) (string, bool) {
	value, ok := osLookupEnv(name)
	if !ok || strings.TrimSpace(value) == "" {
		return "", false
	}
	return value, true
}

// MapEnvLookup returns an EnvLookup backed by a static map.
func MapEnvLookup(values map[string]string) EnvLookup {
	if len(values) == 0 {
		return nil
	}
	copied := make(map[string]string, len(values))
	for key, value := range values {
		if strings.TrimSpace(value) != "" {
			copied[key] = value
		}
	}
	if len(copied) == 0 {
		return nil
	}
	return func(name string) (string, bool) {
		value, ok := copied[name]
		if !ok || strings.TrimSpace(value) == "" {
			return "", false
		}
		return value, true
	}
}

// osLookupEnv is a test seam; production uses os.LookupEnv.
var osLookupEnv = func(name string) (string, bool) {
	return os.LookupEnv(name)
}
