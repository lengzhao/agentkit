package config

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var (
	envInterpolationRe  = regexp.MustCompile(`^\$\{env:([A-Za-z_][A-Za-z0-9_]*)(?::-([^}]*))?\}$`)
	fileInterpolationRe = regexp.MustCompile(`^\$\{file:(.+?)(:-)?\}$`)
)

type missingInterpolation struct {
	field string
	ref   string
}

func probeInterpolationMissing(instanceID string, node map[string]any, baseDir string) (missingInterpolation, bool) {
	var found missingInterpolation
	ok := walkProbeMap(node, instanceID, baseDir, &found)
	return found, ok
}

func walkProbeMap(m map[string]any, path, baseDir string, found *missingInterpolation) bool {
	for key, value := range m {
		fieldPath := path + "." + key
		switch v := value.(type) {
		case map[string]any:
			if walkProbeMap(v, fieldPath, baseDir, found) {
				return true
			}
		case []any:
			for i, item := range v {
				if probeListItem(item, fmt.Sprintf("%s[%d]", fieldPath, i), baseDir, found) {
					return true
				}
			}
		case string:
			if missing, ok := probeInterpolation(fieldPath, v, baseDir); ok {
				*found = missing
				return true
			}
		}
	}
	return false
}

func probeListItem(item any, fieldPath, baseDir string, found *missingInterpolation) bool {
	switch v := item.(type) {
	case map[string]any:
		return walkProbeMap(v, fieldPath, baseDir, found)
	case string:
		if missing, ok := probeInterpolation(fieldPath, v, baseDir); ok {
			*found = missing
			return true
		}
	}
	return false
}

func probeInterpolation(path, raw, baseDir string) (missingInterpolation, bool) {
	if !strings.Contains(raw, "${") {
		return missingInterpolation{}, false
	}
	if m := envInterpolationRe.FindStringSubmatch(raw); m != nil {
		if val, ok := os.LookupEnv(m[1]); ok && val != "" {
			return missingInterpolation{}, false
		}
		if envInterpolationHasDefault(raw) {
			return missingInterpolation{}, false
		}
		return missingInterpolation{field: path, ref: "env:" + m[1]}, true
	}
	if m := fileInterpolationRe.FindStringSubmatch(raw); m != nil {
		filePath := strings.TrimSpace(m[1])
		if filePath == "" {
			return missingInterpolation{field: path, ref: "file:<empty>"}, true
		}
		if fileInterpolationOptional(m) {
			return missingInterpolation{}, false
		}
		if !filepath.IsAbs(filePath) {
			filePath = filepath.Join(baseDir, filePath)
		}
		if _, err := os.Stat(filePath); err != nil {
			return missingInterpolation{field: path, ref: "file:" + strings.TrimSpace(m[1])}, true
		}
		return missingInterpolation{}, false
	}
	if strings.Contains(raw, "${env:") || strings.Contains(raw, "${file:") {
		return missingInterpolation{field: path, ref: raw}, true
	}
	return missingInterpolation{}, false
}

func envInterpolationHasDefault(raw string) bool {
	return strings.Contains(raw, ":-")
}

func fileInterpolationOptional(m []string) bool {
	return len(m) > 2 && m[2] == ":-"
}

func interpolateInstances(raw map[string]any, baseDir string) error {
	for id, node := range raw {
		nodeMap, ok := asStringMap(node)
		if !ok {
			return fmt.Errorf("instance %q: node must be an object", id)
		}
		if err := walkInterpolateMap(nodeMap, id, baseDir); err != nil {
			return err
		}
	}
	return nil
}

func walkInterpolateMap(m map[string]any, path, baseDir string) error {
	for key, value := range m {
		fieldPath := path + "." + key
		switch v := value.(type) {
		case map[string]any:
			if err := walkInterpolateMap(v, fieldPath, baseDir); err != nil {
				return err
			}
		case []any:
			for i, item := range v {
				if err := interpolateListItem(v, i, item, fmt.Sprintf("%s[%d]", fieldPath, i), baseDir); err != nil {
					return err
				}
			}
			m[key] = v
		case string:
			expanded, err := expandInterpolation(fieldPath, v, baseDir)
			if err != nil {
				return err
			}
			m[key] = expanded
		}
	}
	return nil
}

func interpolateListItem(list []any, index int, item any, fieldPath, baseDir string) error {
	switch v := item.(type) {
	case map[string]any:
		return walkInterpolateMap(v, fieldPath, baseDir)
	case string:
		expanded, err := expandInterpolation(fieldPath, v, baseDir)
		if err != nil {
			return err
		}
		list[index] = expanded
	}
	return nil
}

func expandInterpolation(path, raw, baseDir string) (string, error) {
	if !strings.Contains(raw, "${") {
		return raw, nil
	}
	if m := envInterpolationRe.FindStringSubmatch(raw); m != nil {
		if val, ok := os.LookupEnv(m[1]); ok && val != "" {
			return val, nil
		}
		if envInterpolationHasDefault(raw) {
			if len(m) > 2 {
				return m[2], nil
			}
			return "", nil
		}
		return "", fmt.Errorf("%s: environment variable %q is not set", path, m[1])
	}
	if m := fileInterpolationRe.FindStringSubmatch(raw); m != nil {
		filePath := strings.TrimSpace(m[1])
		if filePath == "" {
			return "", fmt.Errorf("%s: file interpolation path is empty", path)
		}
		if !filepath.IsAbs(filePath) {
			filePath = filepath.Join(baseDir, filePath)
		}
		data, err := os.ReadFile(filePath)
		if err != nil {
			if fileInterpolationOptional(m) {
				return "", nil
			}
			return "", fmt.Errorf("%s: read file %q: %w", path, filePath, err)
		}
		return string(data), nil
	}
	if strings.Contains(raw, "${env:") || strings.Contains(raw, "${file:") {
		return "", fmt.Errorf("%s: unsupported interpolation expression %q", path, raw)
	}
	return raw, nil
}
