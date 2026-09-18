package sessbind

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/workspace"
	"github.com/lengzhao/agentkit/runtime/configfile"
)

const globalRuntimeRel = "global:runtime.json"

// GlobalRuntimeData holds workspace-wide dynamic overrides.
type GlobalRuntimeData struct {
	AgentID agentkit.AgentID  `json:"agentId,omitempty"`
	Models  map[string]string `json:"models,omitempty"`
}

type globalRuntimeCacheEntry struct {
	data    GlobalRuntimeData
	modTime time.Time
	missing bool
}

var globalRuntimeCache sync.Map

func normalizeGlobalRuntime(data GlobalRuntimeData) GlobalRuntimeData {
	if data.Models == nil {
		data.Models = map[string]string{}
	}
	data.AgentID = agentkit.AgentID(strings.TrimSpace(string(data.AgentID)))
	return data
}

func loadGlobalRuntime(ctx context.Context, ws workspace.Service) (GlobalRuntimeData, error) {
	if ws == nil {
		return GlobalRuntimeData{Models: map[string]string{}}, nil
	}
	path, err := ws.Resolve(ctx, globalRuntimeRel)
	if err != nil {
		return GlobalRuntimeData{}, err
	}
	info, statErr := os.Stat(path)
	if statErr == nil {
		if v, ok := globalRuntimeCache.Load(path); ok {
			entry := v.(globalRuntimeCacheEntry)
			if !entry.missing && entry.modTime.Equal(info.ModTime()) {
				return entry.data, nil
			}
		}
	} else if errors.Is(statErr, os.ErrNotExist) {
		if v, ok := globalRuntimeCache.Load(path); ok {
			entry := v.(globalRuntimeCacheEntry)
			if entry.missing {
				return entry.data, nil
			}
		}
	} else {
		return GlobalRuntimeData{}, statErr
	}

	var data GlobalRuntimeData
	missing := errors.Is(statErr, os.ErrNotExist)
	if !missing {
		raw, err := os.ReadFile(path)
		if err != nil {
			return GlobalRuntimeData{}, err
		}
		if err := json.Unmarshal(raw, &data); err != nil {
			return GlobalRuntimeData{}, err
		}
	}
	data = normalizeGlobalRuntime(data)

	modTime := time.Time{}
	if statErr == nil {
		modTime = info.ModTime()
	}
	globalRuntimeCache.Store(path, globalRuntimeCacheEntry{data: data, modTime: modTime, missing: missing})
	return data, nil
}

func saveGlobalRuntime(ctx context.Context, ws workspace.Service, data GlobalRuntimeData) error {
	if ws == nil {
		return errors.New("workspace is required")
	}
	data.AgentID = agentkit.AgentID(strings.TrimSpace(string(data.AgentID)))
	if data.Models != nil {
		clean := make(map[string]string, len(data.Models))
		for k, v := range data.Models {
			k = strings.TrimSpace(k)
			v = strings.TrimSpace(v)
			if k == "" || v == "" {
				continue
			}
			clean[k] = v
		}
		data.Models = clean
	}

	path, err := ws.Resolve(ctx, globalRuntimeRel)
	if err != nil {
		return err
	}
	if data.AgentID == "" && len(data.Models) == 0 {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		globalRuntimeCache.Delete(path)
		return nil
	}
	raw, err := json.Marshal(data)
	if err != nil {
		return err
	}
	if err := configfile.WriteAtomic(path, raw, 0o644); err != nil {
		return err
	}
	globalRuntimeCache.Delete(path)
	return nil
}
