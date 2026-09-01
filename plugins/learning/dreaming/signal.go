package dreaming

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Signal is one grounded short-term memory candidate.
type Signal struct {
	ID               string    `json:"id"`
	Text             string    `json:"text"`
	Source           string    `json:"source"`
	SessionID        string    `json:"sessionId,omitempty"`
	RecallCount      int       `json:"recallCount"`
	SessionSources   []string  `json:"sessionSources,omitempty"`
	FirstSeen        time.Time `json:"firstSeen"`
	LastSeen         time.Time `json:"lastSeen"`
	LightHits        int       `json:"lightHits,omitempty"`
	REMHits          int       `json:"remHits,omitempty"`
	GroundedBackfill bool      `json:"groundedBackfill,omitempty"`
}

// State persists dreaming machine state on disk.
type State struct {
	Enabled         bool                 `json:"enabled"`
	LastSweep       time.Time            `json:"lastSweep,omitempty"`
	Signals         []Signal             `json:"signals"`
	ProcessedEvents map[string]time.Time `json:"processedEvents,omitempty"`
	PromotedToday   int                  `json:"promotedToday,omitempty"`
	PromotedDate    string               `json:"promotedDate,omitempty"`
}

// Store reads and writes dreaming state.json.
type Store struct {
	Path string
}

func (s *Store) Load() (*State, error) {
	if s.Path == "" {
		return nil, fmt.Errorf("dreaming state path is required")
	}
	data, err := os.ReadFile(s.Path)
	if err != nil {
		if os.IsNotExist(err) {
			return &State{Enabled: true, ProcessedEvents: map[string]time.Time{}}, nil
		}
		return nil, err
	}
	var st State
	if err := json.Unmarshal(data, &st); err != nil {
		return nil, err
	}
	if st.Signals == nil {
		st.Signals = []Signal{}
	}
	if st.ProcessedEvents == nil {
		st.ProcessedEvents = map[string]time.Time{}
	}
	return &st, nil
}

func (s *Store) Save(st *State) error {
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.Path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.Path)
}

// UpsertSignal merges one grounded signal into state.
func (st *State) UpsertSignal(sig Signal, now time.Time) {
	st.upsertSignal(sig, now)
}

func (st *State) upsertSignal(sig Signal, now time.Time) {
	sig.Text = strings.TrimSpace(sig.Text)
	if sig.Text == "" || sig.Source == "" {
		return
	}
	key := signalKey(sig.Text)
	for i := range st.Signals {
		if signalKey(st.Signals[i].Text) != key {
			continue
		}
		st.Signals[i].RecallCount++
		st.Signals[i].LastSeen = now
		if sig.SessionID != "" && !containsString(st.Signals[i].SessionSources, sig.SessionID) {
			st.Signals[i].SessionSources = append(st.Signals[i].SessionSources, sig.SessionID)
		}
		return
	}
	if sig.ID == "" {
		sig.ID = fmt.Sprintf("sig-%d", now.UnixNano())
	}
	if sig.FirstSeen.IsZero() {
		sig.FirstSeen = now
	}
	sig.LastSeen = now
	if sig.RecallCount <= 0 {
		sig.RecallCount = 1
	}
	if sig.SessionID != "" {
		sig.SessionSources = []string{sig.SessionID}
	}
	st.Signals = append(st.Signals, sig)
}

func signalKey(text string) string {
	return strings.ToLower(strings.Join(strings.Fields(text), " "))
}

func containsString(ss []string, v string) bool {
	for _, s := range ss {
		if s == v {
			return true
		}
	}
	return false
}
