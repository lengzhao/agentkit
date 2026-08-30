package multiplex

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"sync"

	"github.com/lengzhao/agentkit"
)

type Config struct{}

type Deps struct {
	Platforms []agentkit.Platform `json:"platforms"`
}

type Platform struct {
	platforms map[string]agentkit.Platform
	inbox     chan incoming
	active    map[string]bool
	startOnce sync.Once
	mu        sync.Mutex
}

type incoming struct {
	id    string
	event agentkit.MessageEvent
	err   error
}

// New registers platform/multiplex: Merge several platforms into one Runner entrypoint.
//
// Best practices:
//   - Raise runner.maxConcurrentTurns if the merged platforms should make progress in parallel.
func New(cfg Config, deps Deps) (agentkit.Platform, error) {
	if len(deps.Platforms) == 0 {
		return nil, fmt.Errorf("multiplex requires at least one platform")
	}
	platforms, err := assignPlatformIDs(deps.Platforms)
	if err != nil {
		return nil, err
	}
	active := make(map[string]bool, len(platforms))
	for id := range platforms {
		active[id] = true
	}
	return &Platform{
		platforms: platforms,
		inbox:     make(chan incoming, len(platforms)),
		active:    active,
	}, nil
}

func assignPlatformIDs(platforms []agentkit.Platform) (map[string]agentkit.Platform, error) {
	out := make(map[string]agentkit.Platform, len(platforms))
	used := make(map[string]struct{}, len(platforms))
	for i, p := range platforms {
		if p == nil {
			return nil, fmt.Errorf("platforms[%d] is nil", i)
		}
		base, err := leafPlatformID(p, i)
		if err != nil {
			return nil, err
		}
		id := uniquePlatformKey(base, i, used)
		used[id] = struct{}{}
		out[id] = p
	}
	return out, nil
}

func leafPlatformID(p agentkit.Platform, index int) (string, error) {
	ider, ok := p.(agentkit.PlatformIdentifier)
	if !ok {
		return "", fmt.Errorf("platforms[%d] does not implement PlatformIdentifier", index)
	}
	id := ider.PlatformID()
	if id == "" {
		return "", fmt.Errorf("platforms[%d] has empty PlatformID", index)
	}
	return id, nil
}

func uniquePlatformKey(base string, index int, used map[string]struct{}) string {
	if _, exists := used[base]; !exists {
		return base
	}
	for n := index; ; n++ {
		candidate := base + strconv.Itoa(n)
		if _, exists := used[candidate]; !exists {
			return candidate
		}
	}
}

func (m *Platform) Receive(ctx context.Context) (agentkit.MessageEvent, error) {
	m.startOnce.Do(func() {
		for id, p := range m.platforms {
			go m.readPlatform(ctx, id, p)
		}
	})

	for {
		select {
		case <-ctx.Done():
			return agentkit.MessageEvent{}, ctx.Err()
		case msg := <-m.inbox:
			if msg.err != nil {
				if errors.Is(msg.err, io.EOF) {
					m.markInactive(msg.id)
					if m.allInactive() {
						return agentkit.MessageEvent{}, io.EOF
					}
					continue
				}
				return agentkit.MessageEvent{}, msg.err
			}
			return msg.event, nil
		}
	}
}

func (m *Platform) readPlatform(ctx context.Context, id string, p agentkit.Platform) {
	for {
		event, err := p.Receive(ctx)
		if err != nil {
			m.inbox <- incoming{id: id, err: err}
			return
		}
		event.PlatformID = id
		m.inbox <- incoming{id: id, event: event}
	}
}

func (m *Platform) Send(ctx context.Context, out agentkit.OutboundEvent) error {
	if err := out.RequirePlatformID(); err != nil {
		return err
	}
	p, ok := m.platforms[out.PlatformID]
	if !ok {
		return fmt.Errorf("unknown platform %q", out.PlatformID)
	}
	return p.Send(ctx, out)
}

func (m *Platform) markInactive(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.active[id] = false
}

func (m *Platform) isActive(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.active[id]
}

func (m *Platform) allInactive() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, ok := range m.active {
		if ok {
			return false
		}
	}
	return true
}
