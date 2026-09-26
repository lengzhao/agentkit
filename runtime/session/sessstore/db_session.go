package sessstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"sync"

	"github.com/lengzhao/agentkit"
)

type dbSession struct {
	mu              sync.Mutex
	id              agentkit.SessionID
	db              *sql.DB
	driver          string
	seq             agentkit.EventSeq
	maxLoadedEvents int
	mem             *Memory
}

func newDBSession(db *sql.DB, driver string, id agentkit.SessionID, maxLoadedEvents int) (*dbSession, error) {
	mem, err := newMemory(MemoryConfig{ID: id})
	if err != nil {
		return nil, err
	}
	s := &dbSession{
		id:              id,
		db:              db,
		driver:          driver,
		maxLoadedEvents: maxLoadedEvents,
		mem:             mem,
	}
	if err := s.loadExisting(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *dbSession) loadExisting() error {
	rows, err := s.db.Query(SQLPlaceholderQuery(s.driver,
		`SELECT payload FROM ak_session_events WHERE session_id = ? ORDER BY seq`), string(s.id))
	if err != nil {
		return err
	}
	defer rows.Close()

	var all []agentkit.SessionEvent
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return err
		}
		var ev agentkit.SessionEvent
		if err := json.Unmarshal(raw, &ev); err != nil {
			return err
		}
		all = append(all, ev)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	events, maxSeq, trimmed, err := FoldLoadedSessionEvents(all, s.maxLoadedEvents)
	if err != nil {
		return err
	}
	s.seq = maxSeq
	s.mem.setLoadedEvents(events, cutoffsFromCompactions(compactionEvents(events)), trimmed)
	s.mem.resumeSeq(s.seq)
	return nil
}

func (s *dbSession) ID() agentkit.SessionID { return s.id }

func (s *dbSession) Append(ctx context.Context, event agentkit.SessionEvent) (agentkit.EventSeq, error) {
	s.mu.Lock()
	seq, err := s.mem.appendLocked(ctx, event)
	if err != nil {
		s.mu.Unlock()
		return 0, err
	}
	ev := event
	ev.Seq = seq
	raw, err := json.Marshal(ev)
	if err != nil {
		s.mem.rollbackAppendLocked(seq)
		s.mu.Unlock()
		return 0, err
	}
	s.mu.Unlock()

	_, err = s.db.ExecContext(ctx, SQLPlaceholderQuery(s.driver,
		`INSERT INTO ak_session_events (session_id, seq, payload) VALUES (?, ?, ?)`),
		string(s.id), int64(seq), raw,
	)
	if err != nil {
		s.mu.Lock()
		s.mem.rollbackAppendLocked(seq)
		s.mu.Unlock()
		return 0, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.seq = seq
	s.mem.trimIfCompaction(ev)
	return seq, nil
}

func (s *dbSession) Read(ctx context.Context, from agentkit.EventSeq) ([]agentkit.SessionEvent, error) {
	s.mu.Lock()
	if from == 0 && s.mem.isTrimmed() {
		s.mu.Unlock()
		return s.readAllFromDB(ctx, from)
	}
	out := s.mem.readUnlocked(from)
	s.mu.Unlock()
	return out, nil
}

func (s *dbSession) readAllFromDB(ctx context.Context, from agentkit.EventSeq) ([]agentkit.SessionEvent, error) {
	rows, err := s.db.QueryContext(ctx, SQLPlaceholderQuery(s.driver,
		`SELECT payload FROM ak_session_events WHERE session_id = ? AND seq > ? ORDER BY seq`),
		string(s.id), int64(from),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]agentkit.SessionEvent, 0)
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var ev agentkit.SessionEvent
		if err := json.Unmarshal(raw, &ev); err != nil {
			return nil, err
		}
		out = append(out, ev)
	}
	return out, rows.Err()
}

func (s *dbSession) DeriveMessages(ctx context.Context) ([]agentkit.ModelMessage, error) {
	return s.mem.DeriveMessages(ctx)
}
