package credentials

import (
	"fmt"
	"sort"
	"strings"
)

func parseScopedStorageKey(storageKey string) (scope, envKey string, ok bool) {
	i := strings.Index(storageKey, scopedStorageSep)
	if i <= 0 {
		return "", "", false
	}
	scope = storageKey[:i]
	envKey = storageKey[i+len(scopedStorageSep):]
	if envKey == "" || validateIntegrationScope(scope) != nil {
		return "", "", false
	}
	return scope, envKey, true
}

func sortedMapKeys(m map[string]string) []string {
	if len(m) == 0 {
		return nil
	}
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func (s *envStore) appendKeyInventory(b *strings.Builder) {
	s.mu.RLock()
	encKeys := sortedMapKeys(s.encrypted)
	fileKeys := sortedMapKeys(s.files)
	abnormal := append([]string(nil), s.abnormalEncrypted...)
	s.mu.RUnlock()

	appendKeySection(b, "loaded encrypted keys", encKeys)
	appendKeySection(b, "abnormal encrypted keys", abnormal)
	appendKeySection(b, "dotenv keys", fileKeys)
}

func appendKeySection(b *strings.Builder, title string, keys []string) {
	if len(keys) == 0 {
		return
	}
	b.WriteString("\n")
	b.WriteString(title)
	b.WriteString(":")
	for _, key := range keys {
		b.WriteString("\n  - ")
		b.WriteString(key)
	}
}

type scopedEnvKeyView struct {
	envKey    string
	loaded    bool
	abnormal  bool
	config    bool
	manifest  bool
}

func (s *integrationStore) formatScopedEnvKeyInventory() string {
	scopes := s.collectIntegrationScopes()
	if len(scopes) == 0 {
		return ""
	}
	abnormalSet := make(map[string]struct{})
	s.mu.RLock()
	for _, k := range s.abnormalEncrypted {
		abnormalSet[k] = struct{}{}
	}
	s.mu.RUnlock()

	var b strings.Builder
	b.WriteString("\nscoped env keys:")
	for _, scope := range scopes {
		views := s.envKeyViewsForScope(scope, abnormalSet)
		if len(views) == 0 {
			continue
		}
		b.WriteString("\n  ")
		b.WriteString(scope)
		b.WriteString(":")
		for _, v := range views {
			b.WriteString("\n    - ")
			b.WriteString(v.envKey)
			b.WriteString(" [")
			b.WriteString(strings.Join(v.statusTags(), ", "))
			b.WriteString("]")
		}
	}
	return b.String()
}

func (v scopedEnvKeyView) statusTags() []string {
	var tags []string
	if v.manifest {
		tags = append(tags, "manifest")
	}
	if v.loaded {
		tags = append(tags, "loaded")
	}
	if v.config {
		tags = append(tags, "config")
	}
	if v.abnormal {
		tags = append(tags, "abnormal")
	}
	if len(tags) == 0 {
		tags = append(tags, "unknown")
	}
	return tags
}

func (s *integrationStore) collectIntegrationScopes() []string {
	seen := make(map[string]struct{})
	s.mu.RLock()
	for scope := range s.allow {
		seen[scope] = struct{}{}
	}
	for storageKey := range s.encrypted {
		if scope, _, ok := parseScopedStorageKey(storageKey); ok {
			seen[scope] = struct{}{}
		}
	}
	for storageKey := range s.files {
		if scope, _, ok := parseScopedStorageKey(storageKey); ok {
			seen[scope] = struct{}{}
		}
	}
	for storageKey := range s.configScoped {
		if scope, _, ok := parseScopedStorageKey(storageKey); ok {
			seen[scope] = struct{}{}
		}
	}
	for _, storageKey := range s.abnormalEncrypted {
		if scope, _, ok := parseScopedStorageKey(storageKey); ok {
			seen[scope] = struct{}{}
		}
	}
	s.mu.RUnlock()
	out := make([]string, 0, len(seen))
	for scope := range seen {
		out = append(out, scope)
	}
	sort.Strings(out)
	return out
}

func (s *integrationStore) envKeyViewsForScope(scope string, abnormalSet map[string]struct{}) []scopedEnvKeyView {
	byKey := make(map[string]*scopedEnvKeyView)
	s.mu.RLock()
	for k := range s.allow[scope] {
		v := byKey[k]
		if v == nil {
			v = &scopedEnvKeyView{envKey: k}
			byKey[k] = v
		}
		v.manifest = true
	}
	prefix := scopedStorageKey(scope, "")
	markStorage := func(m map[string]string, loaded, config bool) {
		for storageKey := range m {
			if !strings.HasPrefix(storageKey, prefix) {
				continue
			}
			envKey := strings.TrimPrefix(storageKey, prefix)
			if p := strings.TrimSpace(s.prefix); p != "" && strings.HasPrefix(envKey, p) {
				envKey = strings.TrimPrefix(envKey, p)
			}
			envKey = strings.TrimSpace(envKey)
			if envKey == "" {
				continue
			}
			v := byKey[envKey]
			if v == nil {
				v = &scopedEnvKeyView{envKey: envKey}
				byKey[envKey] = v
			}
			if loaded {
				v.loaded = true
			}
			if config {
				v.config = true
			}
		}
	}
	markStorage(s.encrypted, true, false)
	markStorage(s.configScoped, false, true)
	markStorage(s.files, true, false)
	for storageKey := range abnormalSet {
		if !strings.HasPrefix(storageKey, prefix) {
			continue
		}
		envKey := strings.TrimPrefix(storageKey, prefix)
		if p := strings.TrimSpace(s.prefix); p != "" && strings.HasPrefix(envKey, p) {
			envKey = strings.TrimPrefix(envKey, p)
		}
		envKey = strings.TrimSpace(envKey)
		if envKey == "" {
			continue
		}
		v := byKey[envKey]
		if v == nil {
			v = &scopedEnvKeyView{envKey: envKey}
			byKey[envKey] = v
		}
		v.abnormal = true
	}
	s.mu.RUnlock()

	out := make([]scopedEnvKeyView, 0, len(byKey))
	for _, v := range byKey {
		out = append(out, *v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].envKey < out[j].envKey })
	return out
}

// formatFlatEncryptedKeys lists encrypted storage keys that are not scoped integration keys.
func (s *envStore) formatFlatEncryptedKeys() string {
	s.mu.RLock()
	var flat []string
	for storageKey := range s.encrypted {
		if _, _, ok := parseScopedStorageKey(storageKey); ok {
			continue
		}
		flat = append(flat, storageKey)
	}
	s.mu.RUnlock()
	sort.Strings(flat)
	if len(flat) == 0 {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "\nflat encrypted keys:")
	for _, key := range flat {
		b.WriteString("\n  - ")
		b.WriteString(key)
	}
	return b.String()
}
