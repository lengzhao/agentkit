package credentials

import (
	"fmt"
	"sort"
	"strings"
)

func buildConfigScoped(cfgPrefix string, scopedEnv map[string]map[string]string) (map[string]string, error) {
	if len(scopedEnv) == 0 {
		return nil, nil
	}
	out := make(map[string]string)
	for scope, kv := range scopedEnv {
		scope = strings.TrimSpace(scope)
		if err := validateIntegrationScope(scope); err != nil {
			return nil, fmt.Errorf("scopedEnv[%q]: %w", scope, err)
		}
		for envKey, value := range kv {
			envKey = strings.TrimSpace(envKey)
			if envKey == "" {
				continue
			}
			storageKey := envKey
			if p := strings.TrimSpace(cfgPrefix); p != "" {
				storageKey = p + envKey
			}
			scoped := scopedStorageKey(scope, storageKey)
			out[scoped] = strings.TrimSpace(value)
		}
	}
	return out, nil
}

func (s *envStore) setConfigScoped(scoped map[string]string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(scoped) == 0 {
		s.configScoped = nil
		return
	}
	s.configScoped = scoped
}

func (s *envStore) listEnvKeysForScope(scope string) []string {
	scope = strings.TrimSpace(scope)
	if scope == "" {
		return nil
	}
	prefix := scopedStorageKey(scope, "")
	seen := make(map[string]struct{})
	s.mu.RLock()
	defer s.mu.RUnlock()
	collect := func(m map[string]string) {
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
			seen[envKey] = struct{}{}
		}
	}
	collect(s.encrypted)
	collect(s.files)
	collect(s.configScoped)
	if len(seen) == 0 {
		return nil
	}
	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
