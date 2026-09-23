package agentkit

import (
	"slices"
	"strings"
)

// Input modalities for LLM requests (OpenAI-style content parts).
const (
	ModalityText  = "text"
	ModalityImage = "image"
	ModalityAudio = "audio"
)

// DefaultLLMModalities is assumed when a provider does not implement ModalityAwareLLM.
var DefaultLLMModalities = []string{ModalityText, ModalityImage}

// ModalityAwareLLM is optional on LLMProvider implementations. When absent, the
// runtime assumes DefaultLLMModalities.
type ModalityAwareLLM interface {
	Modalities() []string
}

// ModelModalityAwareLLM is optional. When implemented, the agent runtime uses
// ModalitiesForModel for the active agent model instead of Modalities().
type ModelModalityAwareLLM interface {
	ModalitiesForModel(model string) []string
}

// NormalizeModalities trims, lowercases, deduplicates, and drops unknown values.
// Empty input returns DefaultLLMModalities.
func NormalizeModalities(in []string) []string {
	if len(in) == 0 {
		return slices.Clone(DefaultLLMModalities)
	}
	seen := make(map[string]struct{}, len(in))
	var out []string
	for _, raw := range in {
		m := normalizeModality(raw)
		if m == "" {
			continue
		}
		if _, ok := seen[m]; ok {
			continue
		}
		seen[m] = struct{}{}
		out = append(out, m)
	}
	if len(out) == 0 {
		return slices.Clone(DefaultLLMModalities)
	}
	return out
}

func normalizeModality(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case ModalityText, "txt":
		return ModalityText
	case ModalityImage, "vision", "image_url":
		return ModalityImage
	case ModalityAudio, "voice", "speech":
		return ModalityAudio
	default:
		return ""
	}
}

// SupportsModality reports whether normalized modalities include m.
func SupportsModality(modalities []string, m string) bool {
	m = normalizeModality(m)
	if m == "" {
		return false
	}
	for _, item := range NormalizeModalities(modalities) {
		if item == m {
			return true
		}
	}
	return false
}
