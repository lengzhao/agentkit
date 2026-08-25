package settings

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/lengzhao/agentkit/cap/settings"
	"github.com/lengzhao/pluginkit"
	"gopkg.in/yaml.v3"
)

type Config struct {
	// Path is settings file, resolved through the workspace.
	Path string `json:"path"`
}

type Store struct {
	path string
	data map[string]any
}

func init() {
	pluginkit.Register("settings/file", New)
}

// New registers settings/file: Load persistent settings from a YAML or JSON file.
func New(cfg Config) (settings.Store, error) {
	if cfg.Path == "" {
		return nil, fmt.Errorf("settings/file requires path")
	}
	store := &Store{path: cfg.Path}
	if err := store.load(); err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	return store, nil
}

func (s *Store) load() error {
	raw, err := os.ReadFile(s.path)
	if err != nil {
		return err
	}
	var data map[string]any
	if strings.HasSuffix(strings.ToLower(s.path), ".json") {
		if err := json.Unmarshal(raw, &data); err != nil {
			return err
		}
	} else {
		if err := yaml.Unmarshal(raw, &data); err != nil {
			return err
		}
	}
	s.data = data
	return nil
}

func (s *Store) Get(_ context.Context, key string) (settings.Value, error) {
	if s.data == nil {
		return settings.Value{}, fmt.Errorf("settings key %q not found", key)
	}
	value, ok := lookup(s.data, strings.Split(key, "."))
	if !ok {
		return settings.Value{}, fmt.Errorf("settings key %q not found", key)
	}
	return settings.Value{Raw: value}, nil
}

func lookup(data map[string]any, parts []string) (any, bool) {
	if len(parts) == 0 {
		return nil, false
	}
	current, ok := data[parts[0]]
	if !ok {
		return nil, false
	}
	if len(parts) == 1 {
		return current, true
	}
	next, ok := current.(map[string]any)
	if !ok {
		return nil, false
	}
	return lookup(next, parts[1:])
}
