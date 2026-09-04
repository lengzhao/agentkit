package config

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/pluginkit"
	"gopkg.in/yaml.v3"
)

// ToolProfile selects which tools/runtime instance graph fragment to scaffold.
type ToolProfile string

const (
	ToolProfileDefault  ToolProfile = "default"
	ToolProfileSubagent ToolProfile = "subagent"
)

// ToolsScaffoldOptions controls kind filtering when generating tool graphs.
type ToolsScaffoldOptions struct {
	Whitelist []string
	Blacklist []string
}

// DefaultToolBlacklist excludes test, scripted, and alternate implementations from the main agent toolset.
func DefaultToolBlacklist() []string {
	return []string{
		"tool/fs-memory",
		"tool/web-search-auto",
		"tool/web-search-scripted",
		"tool/web-fetch-scripted",
		"tool/web-search-tavily",
		"tool/web-search-duckduckgo",
		"tool/web-search-exa",
		"tool/todo",
		"tool/finish",
	}
}

// DefaultSubagentToolWhitelist is the explicit in-process subagent tool surface.
// New tool plugins do not appear here until deliberately added.
func DefaultSubagentToolWhitelist() []string {
	return []string{
		"tool/finish",
		"tool/web-fetch-http",
		"tool/fs-workspace",
	}
}

type toolSlot struct {
	name      string
	want      reflect.Type
	instances map[string]toolInstanceSpec
}

type toolInstanceSpec struct {
	ID     string
	Config map[string]any
	Deps   map[string]any
}

// ScaffoldToolsFragment generates a pluginkit instance map for one tools/runtime profile.
func ScaffoldToolsFragment(profile ToolProfile, opts ToolsScaffoldOptions) (map[string]any, error) {
	whitelist := opts.Whitelist
	blacklist := opts.Blacklist
	switch profile {
	case ToolProfileSubagent:
		if len(whitelist) == 0 {
			whitelist = DefaultSubagentToolWhitelist()
		}
	default:
		if len(whitelist) == 0 && len(blacklist) == 0 {
			blacklist = DefaultToolBlacklist()
		}
	}

	specs := toolInstanceSpecs(profile)
	slots := []toolSlot{
		{name: "tools", want: reflect.TypeOf((*agentkit.Tool)(nil)).Elem(), instances: specs},
		{name: "toolPacks", want: reflect.TypeOf((*agentkit.ToolPack)(nil)).Elem(), instances: specs},
		{name: "dynamicTools", want: reflect.TypeOf((*agentkit.ToolProvider)(nil)).Elem(), instances: specs},
	}

	runtimeID, runtimeNode, err := runtimeNodeForProfile(profile)
	if err != nil {
		return nil, err
	}

	out := make(map[string]any)
	deps := map[string]any{
		"hooks":    "hooks.default",
		"policies": []any{"policy.dangerous-shell.default"},
	}
	for _, slot := range slots {
		kinds := filterKinds(pluginkit.CompatibleKinds(slot.want), whitelist, blacklist)
		kinds = filterToolKinds(kinds, slot.want)
		refs, err := materializeKinds(kinds, slot.instances, out)
		if err != nil {
			return nil, err
		}
		if len(refs) > 0 {
			deps[slot.name] = refs
		}
	}
	runtimeNode["deps"] = deps
	out[runtimeID] = runtimeNode
	return out, nil
}

// ScaffoldToolsYAML returns a YAML fragment for the given tools/runtime profile.
func ScaffoldToolsYAML(profile ToolProfile, opts ToolsScaffoldOptions) ([]byte, error) {
	fragment, err := ScaffoldToolsFragment(profile, opts)
	if err != nil {
		return nil, err
	}
	return yaml.Marshal(fragment)
}

func runtimeNodeForProfile(profile ToolProfile) (string, map[string]any, error) {
	switch profile {
	case ToolProfileDefault:
		return "tools.default", map[string]any{
			"use": "tools/runtime",
			"config": map[string]any{
				"defaultTimeoutSeconds": 120,
				"maxResultBytes":        8192,
				"toolTimeouts": map[string]any{
					"delegate": 900,
					"ask_user": 900,
				},
			},
		}, nil
	case ToolProfileSubagent:
		return "tools.subagent.default", map[string]any{
			"use": "tools/runtime",
			"config": map[string]any{
				"defaultTimeoutSeconds": 120,
				"maxResultBytes":        8192,
			},
		}, nil
	default:
		return "", nil, fmt.Errorf("unknown tool profile %q", profile)
	}
}

func toolInstanceSpecs(profile ToolProfile) map[string]toolInstanceSpec {
	switch profile {
	case ToolProfileSubagent:
		return subagentToolInstanceSpecs()
	default:
		return defaultToolInstanceSpecs()
	}
}

func defaultToolInstanceSpecs() map[string]toolInstanceSpec {
	return map[string]toolInstanceSpec{
		"tool/fs-workspace": {
			ID: "tool.fs-workspace.default",
			Config: map[string]any{
				"root":     ".",
				"maxBytes": 1048576,
			},
			Deps: map[string]any{"workspace": "workspace.default"},
		},
		"tool/shell-bash": {
			ID: "tool.shell-bash.default",
			Config: map[string]any{
				"workDir":        "work",
				"timeoutSeconds": 60,
			},
			Deps: map[string]any{"workspace": "workspace.default"},
		},
		"tool/skill": {
			ID: "tool.skill.default",
			Deps: map[string]any{
				"skills":       "skills.default",
				"sessionStore": "sessionStore.default",
			},
		},
		"tool/schedule": {
			ID: "tool.schedule.default",
			Config: map[string]any{
				"maxJobs": 32,
			},
			Deps: map[string]any{"schedule": "schedule.default"},
		},
		"tool/subagent": {
			ID:   "tool.subagent.default",
			Deps: map[string]any{"subagent": "subagent.default"},
		},
		"tool/finish": {
			ID:   "tool.finish.default",
			Deps: map[string]any{"sessionStore": "sessionStore.default"},
		},
		"tool/ask-user": {
			ID: "tool.ask-user.default",
		},
		"tool/send": {
			ID: "tool.send.default",
			Deps: map[string]any{
				"platform":  "platform.default",
				"workspace": "workspace.default",
			},
		},
		"tool/web-fetch-http": {
			ID: "tool.web-fetch-http.default",
			Config: map[string]any{
				"timeoutSeconds":    30,
				"maxBytes":          1048576,
				"maxRedirects":      5,
				"allowPrivateHosts": false,
			},
		},
		"tool/web-search-auto": {
			ID: "tool.web-search.default",
			Config: map[string]any{
				"maxResults":   5,
				"snippetChars": 800,
				"tavily": map[string]any{
					"apiKeyRef":   "env:TAVILY_API_KEY",
					"searchDepth": "basic",
				},
				"duckduckgo": map[string]any{},
			},
			Deps: map[string]any{"credentials": "credentials.default"},
		},
		"tool/mcp": {
			ID: "mcp.default",
			Config: map[string]any{
				"files": []any{"global:mcp.json"},
			},
			Deps: map[string]any{
				"workspace":   "workspace.default",
				"credentials": "credentials.default",
			},
		},
		"tool/openapi": {
			ID: "openapi.default",
			Config: map[string]any{
				"files": []any{"global:api.json"},
			},
			Deps: map[string]any{
				"workspace":   "workspace.default",
				"credentials": "credentials.default",
			},
		},
	}
}

func subagentToolInstanceSpecs() map[string]toolInstanceSpec {
	specs := defaultToolInstanceSpecs()
	specs["tool/fs-workspace"] = toolInstanceSpec{
		ID: "tool.fs-workspace.readonly.default",
		Config: map[string]any{
			"root":     ".",
			"maxBytes": 1048576,
			"readOnly": true,
			"tools":    []any{"read", "grep", "find", "ls"},
		},
		Deps: map[string]any{"workspace": "workspace.default"},
	}
	return specs
}

func materializeKinds(kinds []string, specs map[string]toolInstanceSpec, out map[string]any) ([]string, error) {
	refs := make([]string, 0, len(kinds))
	for _, kind := range kinds {
		spec, ok := specs[kind]
		if !ok {
			spec = toolInstanceSpec{ID: defaultToolInstanceID(kind)}
		}
		node := map[string]any{"use": kind}
		if len(spec.Config) > 0 {
			node["config"] = spec.Config
		}
		if len(spec.Deps) > 0 {
			node["deps"] = spec.Deps
		}
		out[spec.ID] = node
		refs = append(refs, spec.ID)
	}
	return refs, nil
}

func filterToolKinds(kinds []string, want reflect.Type) []string {
	out := make([]string, 0, len(kinds))
	for _, kind := range kinds {
		if !strings.HasPrefix(kind, "tool/") {
			continue
		}
		desc, ok := pluginkit.Describe(kind)
		if !ok {
			continue
		}
		if !returnTypeMatches(want, desc.ReturnType) {
			continue
		}
		out = append(out, kind)
	}
	return out
}

func returnTypeMatches(want, got reflect.Type) bool {
	if want == nil || got == nil {
		return false
	}
	if got == want {
		return true
	}
	if got.Kind() != reflect.Interface && want.Kind() == reflect.Interface && got.Implements(want) {
		return true
	}
	return got.AssignableTo(want)
}

func filterKinds(kinds, whitelist, blacklist []string) []string {
	white := toKindSet(whitelist)
	black := toKindSet(blacklist)
	out := make([]string, 0, len(kinds))
	for _, kind := range kinds {
		if len(whitelist) > 0 && !white[kind] {
			continue
		}
		if black[kind] {
			continue
		}
		out = append(out, kind)
	}
	return out
}

func toKindSet(kinds []string) map[string]bool {
	set := make(map[string]bool, len(kinds))
	for _, kind := range kinds {
		set[kind] = true
	}
	return set
}

func defaultToolInstanceID(kind string) string {
	slug := strings.TrimPrefix(kind, "tool/")
	slug = strings.ReplaceAll(slug, "/", ".")
	return "tool." + slug + ".default"
}
