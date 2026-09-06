package skill

import (
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/lengzhao/agentkit/runtime/markdown"
	"gopkg.in/yaml.v3"
)

const (
	maxSkillNameLen        = 64
	maxSkillDescriptionLen = 1024
	maxCompatibilityLen    = 500
)

var skillNamePattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

// ParseOptions configures Agent Skills frontmatter validation.
type ParseOptions struct {
	// DirName is the parent directory name for bundle skills. When set, name must match it.
	DirName string
}

// InvocationPolicy controls whether a skill appears in model and user catalogs (DSH extension).
type InvocationPolicy struct {
	ModelInvocable bool
	UserInvocable  bool
}

// Parsed is a validated SKILL.md after frontmatter removal.
type Parsed struct {
	Name          string
	Description   string
	License       string
	Compatibility string
	AllowedTools  string
	WhenToUse     string
	Invocation    InvocationPolicy
	Metadata      map[string]string
	Content       string
}

// IsName reports whether name matches the Agent Skills kebab-case grammar.
func IsName(name string) bool {
	return skillNamePattern.MatchString(name)
}

// ParseFile validates SKILL.md content against the Agent Skills spec and deepseek-harness extensions.
func ParseFile(raw string, opts ParseOptions) (Parsed, error) {
	yamlHead, body, ok := markdown.Split(raw)
	if !ok {
		return Parsed{}, fmt.Errorf("missing YAML frontmatter")
	}
	var data map[string]any
	if err := yaml.Unmarshal([]byte(yamlHead), &data); err != nil {
		return Parsed{}, fmt.Errorf("invalid YAML frontmatter: %w", err)
	}
	if data == nil {
		return Parsed{}, fmt.Errorf("frontmatter must be a YAML mapping")
	}

	name, err := requiredString(data, "name", maxSkillNameLen)
	if err != nil {
		return Parsed{}, err
	}
	if !IsName(name) {
		return Parsed{}, fmt.Errorf("invalid skill name %q", name)
	}
	if dirName := strings.TrimSpace(opts.DirName); dirName != "" && name != dirName {
		return Parsed{}, fmt.Errorf("skill name %q must match directory name %q", name, dirName)
	}

	description, err := requiredString(data, "description", maxSkillDescriptionLen)
	if err != nil {
		return Parsed{}, err
	}
	invocation, err := parseInvocationPolicy(data)
	if err != nil {
		return Parsed{}, err
	}

	out := Parsed{
		Name:        name,
		Description: description,
		Invocation:  invocation,
		Content:     strings.TrimSpace(body),
	}
	if license, ok := optionalStringField(data, "license"); ok {
		out.License = license
	}
	if compatibility, err := optionalBoundedString(data, "compatibility", maxCompatibilityLen); err != nil {
		return Parsed{}, err
	} else if compatibility != "" {
		out.Compatibility = compatibility
	}
	if allowedTools, ok := optionalStringField(data, "allowed-tools"); ok {
		out.AllowedTools = allowedTools
	}
	if whenToUse, ok := optionalStringField(data, "whenToUse"); ok {
		out.WhenToUse = whenToUse
	}
	metadata, err := parseMetadata(data)
	if err != nil {
		return Parsed{}, err
	}
	if len(metadata) > 0 {
		out.Metadata = metadata
	}
	return out, nil
}

func requiredString(data map[string]any, key string, maxLen int) (string, error) {
	value, ok := data[key]
	if !ok {
		return "", fmt.Errorf("frontmatter requires %q", key)
	}
	text, ok := value.(string)
	if !ok || strings.TrimSpace(text) == "" {
		return "", fmt.Errorf("frontmatter requires %q", key)
	}
	text = strings.TrimSpace(text)
	if maxLen > 0 && utf8.RuneCountInString(text) > maxLen {
		return "", fmt.Errorf("frontmatter field %q exceeds maximum length %d", key, maxLen)
	}
	return text, nil
}

func optionalStringField(data map[string]any, key string) (string, bool) {
	value, ok := data[key]
	if !ok {
		return "", false
	}
	text, ok := value.(string)
	if !ok || strings.TrimSpace(text) == "" {
		return "", false
	}
	return strings.TrimSpace(text), true
}

func optionalBoundedString(data map[string]any, key string, maxLen int) (string, error) {
	value, ok := data[key]
	if !ok {
		return "", nil
	}
	text, ok := value.(string)
	if !ok || strings.TrimSpace(text) == "" {
		return "", fmt.Errorf("frontmatter field %q must be a non-empty string", key)
	}
	text = strings.TrimSpace(text)
	if utf8.RuneCountInString(text) > maxLen {
		return "", fmt.Errorf("frontmatter field %q exceeds maximum length %d", key, maxLen)
	}
	return text, nil
}

func parseMetadata(data map[string]any) (map[string]string, error) {
	value, ok := data["metadata"]
	if !ok {
		return nil, nil
	}
	raw, ok := value.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("frontmatter field metadata must be a mapping of strings")
	}
	out := make(map[string]string, len(raw))
	for key, item := range raw {
		text, ok := item.(string)
		if !ok {
			return nil, fmt.Errorf("frontmatter metadata.%s must be a string", key)
		}
		out[key] = text
	}
	return out, nil
}

func parseInvocationPolicy(data map[string]any) (InvocationPolicy, error) {
	if err := rejectLegacyInvocationKey(data, "disableModelInvocation", "disable-model-invocation"); err != nil {
		return InvocationPolicy{}, err
	}
	if err := rejectLegacyInvocationKey(data, "modelInvocable", "disable-model-invocation"); err != nil {
		return InvocationPolicy{}, err
	}
	if err := rejectLegacyInvocationKey(data, "userInvocable", "user-invocable"); err != nil {
		return InvocationPolicy{}, err
	}
	disableModel, err := frontmatterBool(data, "disable-model-invocation")
	if err != nil {
		return InvocationPolicy{}, err
	}
	userInvocable, err := frontmatterBool(data, "user-invocable")
	if err != nil {
		return InvocationPolicy{}, err
	}
	return InvocationPolicy{
		ModelInvocable: disableModel == nil || !*disableModel,
		UserInvocable:  userInvocable == nil || *userInvocable,
	}, nil
}

func rejectLegacyInvocationKey(data map[string]any, legacy, canonical string) error {
	if _, ok := data[legacy]; ok {
		return fmt.Errorf("frontmatter field %q is unsupported; use %q", legacy, canonical)
	}
	return nil
}

func frontmatterBool(data map[string]any, key string) (*bool, error) {
	value, ok := data[key]
	if !ok {
		return nil, nil
	}
	switch v := value.(type) {
	case bool:
		out := v
		return &out, nil
	case int:
		switch v {
		case 1:
			out := true
			return &out, nil
		case 0:
			out := false
			return &out, nil
		default:
			return nil, fmt.Errorf("frontmatter field %q must be a boolean", key)
		}
	case string:
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "true", "yes", "on", "1":
			out := true
			return &out, nil
		case "false", "no", "off", "0":
			out := false
			return &out, nil
		default:
			return nil, fmt.Errorf("frontmatter field %q must be a boolean", key)
		}
	default:
		return nil, fmt.Errorf("frontmatter field %q must be a boolean", key)
	}
}
