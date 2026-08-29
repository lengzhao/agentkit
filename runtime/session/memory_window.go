package session

import (
	"encoding/json"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/compaction"
)

func cutoffsFromCompactions(events []agentkit.SessionEvent) map[agentkit.AgentID]agentkit.EventSeq {
	cutoffs := make(map[agentkit.AgentID]agentkit.EventSeq)
	for _, ev := range events {
		if ev.Type != agentkit.EventCompaction {
			continue
		}
		var data compaction.EventData
		if err := json.Unmarshal(ev.Data, &data); err != nil {
			continue
		}
		if cutoff := data.MemoryCutoffSeq(); cutoff > cutoffs[ev.AgentID] {
			cutoffs[ev.AgentID] = cutoff
		}
	}
	return cutoffs
}

func retainInMemory(ev agentkit.SessionEvent, cutoffs map[agentkit.AgentID]agentkit.EventSeq) bool {
	if ev.Type == agentkit.EventCompaction {
		return true
	}
	if ev.AgentID == "" {
		return true
	}
	return ev.Seq > cutoffs[ev.AgentID]
}

func filterMemoryEvents(events []agentkit.SessionEvent, cutoffs map[agentkit.AgentID]agentkit.EventSeq) []agentkit.SessionEvent {
	if len(cutoffs) == 0 {
		return events
	}
	out := make([]agentkit.SessionEvent, 0, len(events))
	for _, ev := range events {
		if retainInMemory(ev, cutoffs) {
			out = append(out, ev)
		}
	}
	return out
}

func mergeLoadedEvents(compactions, recent []agentkit.SessionEvent, cutoffs map[agentkit.AgentID]agentkit.EventSeq) []agentkit.SessionEvent {
	seen := make(map[agentkit.EventSeq]struct{}, len(compactions)+len(recent))
	out := make([]agentkit.SessionEvent, 0, len(compactions)+len(recent))
	appendUnique := func(ev agentkit.SessionEvent) {
		if !retainInMemory(ev, cutoffs) {
			return
		}
		if _, ok := seen[ev.Seq]; ok {
			return
		}
		seen[ev.Seq] = struct{}{}
		out = append(out, ev)
	}
	for _, ev := range compactions {
		appendUnique(ev)
	}
	for _, ev := range recent {
		appendUnique(ev)
	}
	sortEventsBySeq(out)
	return out
}

func sortEventsBySeq(events []agentkit.SessionEvent) {
	if len(events) < 2 {
		return
	}
	for i := 1; i < len(events); i++ {
		ev := events[i]
		j := i - 1
		for j >= 0 && events[j].Seq > ev.Seq {
			events[j+1] = events[j]
			j--
		}
		events[j+1] = ev
	}
}

type eventRing struct {
	max int
	buf []agentkit.SessionEvent
}

func (r *eventRing) add(ev agentkit.SessionEvent) {
	if r.max <= 0 {
		r.buf = append(r.buf, ev)
		return
	}
	if len(r.buf) < r.max {
		r.buf = append(r.buf, ev)
		return
	}
	copy(r.buf, r.buf[1:])
	r.buf[len(r.buf)-1] = ev
}

func (m *Memory) trimCompacted(agentID agentkit.AgentID, beforeSeq agentkit.EventSeq) {
	if beforeSeq == 0 {
		return
	}
	if m.cutoffByAgent == nil {
		m.cutoffByAgent = make(map[agentkit.AgentID]agentkit.EventSeq)
	}
	if beforeSeq > m.cutoffByAgent[agentID] {
		m.cutoffByAgent[agentID] = beforeSeq
	}
	m.events = filterMemoryEvents(m.events, m.cutoffByAgent)
	m.trimmed = true
}

func (m *Memory) setLoadedEvents(events []agentkit.SessionEvent, cutoffs map[agentkit.AgentID]agentkit.EventSeq, trimmed bool) {
	m.events = events
	if len(cutoffs) == 0 {
		m.cutoffByAgent = nil
	} else {
		m.cutoffByAgent = cutoffs
	}
	m.trimmed = trimmed
}

func (m *Memory) isTrimmed() bool {
	return m.trimmed
}

func (m *Memory) latestSeq() agentkit.EventSeq {
	return m.seq
}

func (m *Memory) readUnlocked(from agentkit.EventSeq) []agentkit.SessionEvent {
	out := make([]agentkit.SessionEvent, 0)
	for _, ev := range m.events {
		if ev.Seq > from {
			out = append(out, ev)
		}
	}
	return out
}

// TrimCompacted drops model-visible history superseded by a compaction marker.
func TrimCompacted(s agentkit.Session, agentID agentkit.AgentID, beforeSeq agentkit.EventSeq) {
	switch backing := s.(type) {
	case *Memory:
		backing.mu.Lock()
		defer backing.mu.Unlock()
		backing.trimCompacted(agentID, beforeSeq)
	case *JSONL:
		backing.trimCompacted(agentID, beforeSeq)
	}
}
