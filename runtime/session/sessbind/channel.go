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
	"github.com/lengzhao/agentkit/runtime/filesystem"
)

// channelRuntimeRel is the current channel's agent overlay.
// workspace/tenant resolves local: against the tenant root for this turn's
// workspace key (platform:channel). workspace/default resolves it against the
// single local project root.
const channelRuntimeRel = "local:runtime.json"

// ChannelRuntimeData holds channel-scoped dynamic overrides.
type ChannelRuntimeData struct {
	AgentID agentkit.AgentID `json:"agentId,omitempty"`
}

type channelRuntimeCacheEntry struct {
	data    ChannelRuntimeData
	modTime time.Time
	missing bool
}

var channelRuntimeCache sync.Map

func normalizeChannelRuntime(data ChannelRuntimeData) ChannelRuntimeData {
	data.AgentID = agentkit.AgentID(strings.TrimSpace(string(data.AgentID)))
	return data
}

func loadChannelRuntime(ctx context.Context, ws workspace.Service) (ChannelRuntimeData, error) {
	if ws == nil {
		return ChannelRuntimeData{}, nil
	}
	path, err := ws.Resolve(ctx, channelRuntimeRel)
	if err != nil {
		return ChannelRuntimeData{}, err
	}
	info, statErr := os.Stat(path)
	if statErr == nil {
		if v, ok := channelRuntimeCache.Load(path); ok {
			entry := v.(channelRuntimeCacheEntry)
			if !entry.missing && entry.modTime.Equal(info.ModTime()) {
				return entry.data, nil
			}
		}
	} else if errors.Is(statErr, os.ErrNotExist) {
		if v, ok := channelRuntimeCache.Load(path); ok {
			entry := v.(channelRuntimeCacheEntry)
			if entry.missing {
				return entry.data, nil
			}
		}
	} else {
		return ChannelRuntimeData{}, statErr
	}

	var data ChannelRuntimeData
	missing := errors.Is(statErr, os.ErrNotExist)
	if !missing {
		raw, err := os.ReadFile(path)
		if err != nil {
			return ChannelRuntimeData{}, err
		}
		if err := json.Unmarshal(raw, &data); err != nil {
			return ChannelRuntimeData{}, err
		}
	}
	data = normalizeChannelRuntime(data)

	modTime := time.Time{}
	if statErr == nil {
		modTime = info.ModTime()
	}
	channelRuntimeCache.Store(path, channelRuntimeCacheEntry{data: data, modTime: modTime, missing: missing})
	return data, nil
}

func saveChannelRuntime(ctx context.Context, ws workspace.Service, data ChannelRuntimeData) error {
	if ws == nil {
		return errors.New("workspace is required")
	}
	data = normalizeChannelRuntime(data)
	path, err := ws.Resolve(ctx, channelRuntimeRel)
	if err != nil {
		return err
	}
	if data.AgentID == "" {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		channelRuntimeCache.Delete(path)
		return nil
	}
	raw, err := json.Marshal(data)
	if err != nil {
		return err
	}
	if err := filesystem.WriteAtomic(path, raw, 0o644); err != nil {
		return err
	}
	channelRuntimeCache.Delete(path)
	return nil
}
