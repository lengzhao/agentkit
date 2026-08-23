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

type Config struct {
	Names []string `json:"names,omitempty"`
}

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

func New(cfg Config, deps Deps) (agentkit.Platform, error) {
	if len(deps.Platforms) == 0 {
		return nil, fmt.Errorf("multiplex requires at least one platform")
	}
	platforms := make(map[string]agentkit.Platform, len(deps.Platforms))
	active := make(map[string]bool, len(deps.Platforms))
	for i, p := range deps.Platforms {
		if p == nil {
			return nil, fmt.Errorf("platforms[%d] is nil", i)
		}
		id := platformID(cfg.Names, i)
		if _, exists := platforms[id]; exists {
			return nil, fmt.Errorf("duplicate platform id %q", id)
		}
		platforms[id] = p
		active[id] = true
	}
	return &Platform{
		platforms: platforms,
		inbox:     make(chan incoming, len(platforms)),
		active:    active,
	}, nil
}

func platformID(names []string, index int) string {
	if index < len(names) && names[index] != "" {
		return names[index]
	}
	return strconv.Itoa(index)
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
	if out.PlatformID != "" {
		p, ok := m.platforms[out.PlatformID]
		if !ok {
			return fmt.Errorf("unknown platform %q", out.PlatformID)
		}
		return p.Send(ctx, out)
	}
	var err error
	for id, p := range m.platforms {
		if !m.isActive(id) {
			continue
		}
		copy := out
		copy.PlatformID = id
		err = errors.Join(err, p.Send(ctx, copy))
	}
	return err
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
