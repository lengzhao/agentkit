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
)

// channelRuntimeRel is a legacy channel agent overlay path (workspace/tenant only).
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

func loadChannelRuntimeLegacy(ctx context.Context, ws workspace.Service) (ChannelRuntimeData, error) {
	path, err := legacyChannelRuntimePath(ctx, ws)
	if err != nil {
		return ChannelRuntimeData{}, err
	}
	if path == "" {
		return ChannelRuntimeData{}, nil
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
	data.AgentID = agentkit.AgentID(strings.TrimSpace(string(data.AgentID)))

	modTime := time.Time{}
	if statErr == nil {
		modTime = info.ModTime()
	}
	channelRuntimeCache.Store(path, channelRuntimeCacheEntry{data: data, modTime: modTime, missing: missing})
	return data, nil
}

func clearChannelRuntimeLegacy(ctx context.Context, ws workspace.Service) error {
	path, err := legacyChannelRuntimePath(ctx, ws)
	if err != nil {
		return err
	}
	if path == "" {
		return nil
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	channelRuntimeCache.Delete(path)
	return nil
}

// A shared local/global root has only a global runtime file. Never interpret
// that file as a legacy channel override or remove it when changing a channel.
func legacyChannelRuntimePath(ctx context.Context, ws workspace.Service) (string, error) {
	if ws == nil {
		return "", nil
	}
	local, err := ws.Resolve(ctx, channelRuntimeRel)
	if err != nil {
		return "", err
	}
	global, err := ws.Resolve(ctx, globalRuntimeRel)
	if err != nil {
		return "", err
	}
	if local == global {
		return "", nil
	}
	return local, nil
}
