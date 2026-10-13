package workflow

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/lengzhao/agentkit/cap/workspace"
)

// DefaultDirs are scanned when the plugin config omits dirs; local wins.
var DefaultDirs = []string{workspace.ScopeLocal + ":workflows", workspace.ScopeGlobal + ":workflows"}

// Discover re-reads every configured dir and returns the workflows found,
// sorted by name. Like subagent Definitions, re-reading per call makes edits
// take effect without a reload command. The first dir wins on duplicate names;
// files that fail to parse are collected into errs (and skipped) so one bad
// file does not block the others.
func Discover(ctx context.Context, ws workspace.Service, dirs []string) ([]*Workflow, []error) {
	if len(dirs) == 0 {
		dirs = DefaultDirs
	}
	byName := make(map[string]*Workflow)
	var errs []error
	for _, scoped := range dirs {
		dir, err := ws.Resolve(ctx, scoped)
		if err != nil {
			errs = append(errs, fmt.Errorf("resolve %s: %w", scoped, err))
			continue
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			if !os.IsNotExist(err) {
				errs = append(errs, fmt.Errorf("read %s: %w", scoped, err))
			}
			continue
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
				continue
			}
			name := strings.TrimSuffix(entry.Name(), ".json")
			if _, dup := byName[name]; dup {
				continue
			}
			path := filepath.Join(dir, entry.Name())
			data, err := os.ReadFile(path)
			if err != nil {
				errs = append(errs, fmt.Errorf("read %s: %w", path, err))
				continue
			}
			wf, err := Parse(name, data)
			if err != nil {
				errs = append(errs, fmt.Errorf("%s: %w", path, err))
				continue
			}
			wf.Dir = dir
			wf.Path = path
			byName[name] = wf
		}
	}
	names := make([]string, 0, len(byName))
	for name := range byName {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]*Workflow, 0, len(names))
	for _, name := range names {
		out = append(out, byName[name])
	}
	return out, errs
}

// Find discovers workflows and returns the named one.
func Find(ctx context.Context, ws workspace.Service, dirs []string, name string) (*Workflow, error) {
	workflows, errs := Discover(ctx, ws, dirs)
	for _, wf := range workflows {
		if wf.Name == name {
			return wf, nil
		}
	}
	available := make([]string, 0, len(workflows))
	for _, wf := range workflows {
		available = append(available, wf.Name)
	}
	err := fmt.Errorf("workflow %q not found; available: %s", name, strings.Join(available, ", "))
	if len(errs) > 0 {
		err = fmt.Errorf("%w (discovery errors: %v)", err, errs)
	}
	return nil, err
}
