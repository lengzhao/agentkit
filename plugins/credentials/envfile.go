package credentials

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

func mergeEnvFile(existing []byte, updates map[string]string) ([]byte, error) {
	lines := splitEnvLines(existing)
	seen := make(map[string]struct{}, len(updates))
	var out []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			out = append(out, line)
			continue
		}
		body := strings.TrimSpace(strings.TrimPrefix(trimmed, "export "))
		key, _, ok := strings.Cut(body, "=")
		if !ok {
			out = append(out, line)
			continue
		}
		key = strings.TrimSpace(key)
		if value, ok := updates[key]; ok {
			out = append(out, formatEnvLine(key, value))
			seen[key] = struct{}{}
			continue
		}
		out = append(out, line)
	}
	keys := make([]string, 0, len(updates))
	for key := range updates {
		if _, ok := seen[key]; ok {
			continue
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		out = append(out, formatEnvLine(key, updates[key]))
	}
	if len(out) == 0 {
		return nil, nil
	}
	return []byte(strings.Join(out, "\n") + "\n"), nil
}

func splitEnvLines(data []byte) []string {
	if len(data) == 0 {
		return nil
	}
	lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

func formatEnvLine(key, value string) string {
	if value == "" || strings.ContainsAny(value, " \t#\"'\n\\") {
		return key + "=" + strconv.Quote(value)
	}
	return key + "=" + value
}

func parseEnvUpdates(pairs []string, prefix string) (map[string]string, []string, error) {
	updates := make(map[string]string, len(pairs))
	refs := make([]string, 0, len(pairs))
	for _, pair := range pairs {
		key, value, err := parseEnvPair(pair)
		if err != nil {
			return nil, nil, err
		}
		if value == "" {
			return nil, nil, fmt.Errorf("%s: value is required", key)
		}
		storageKey := key
		if prefix != "" {
			storageKey = prefix + key
		}
		updates[storageKey] = value
		refs = append(refs, "env:"+key)
	}
	return updates, refs, nil
}
