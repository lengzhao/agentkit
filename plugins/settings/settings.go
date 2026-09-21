package settings

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/lengzhao/agentkit/cap/filesystem"
	"github.com/lengzhao/agentkit/cap/settings"
	"github.com/lengzhao/pluginkit"
	"gopkg.in/yaml.v3"
)

type Config struct {
	// Path is settings file, resolved through the injected filesystem (scope prefixes allowed).
	Path string `json:"path"`
}

type Deps struct {
	FS filesystem.Service `json:"fs"`
}

type Store struct {
	path string
	data map[string]any
}

func init() {
	pluginkit.Register("settings/file", New)
}

// New registers settings/file: Load persistent settings from a YAML or JSON file.
func New(cfg Config, deps Deps) (settings.Store, error) {
	if deps.FS == nil {
		return nil, fmt.Errorf("settings/file requires fs dependency")
	}
	if cfg.Path == "" {
		return nil, fmt.Errorf("settings/file requires path")
	}
	store := &Store{path: cfg.Path}
	if err := store.load(context.Background(), deps.FS); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	return store, nil
}

func (s *Store) load(ctx context.Context, fs filesystem.Service) error {
	raw, err := fs.Read(ctx, s.path)
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
