package learning

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/lengzhao/agentkit/runtime/configfile"
)

// ReviewQuota tracks per-day background review runs for throttling.
type ReviewQuota struct {
	Date string `json:"date"`
	Count int   `json:"count"`
}

// QuotaStore persists review_quota.json under memory/dreaming/.
type QuotaStore struct {
	Path string
}

func (q *QuotaStore) TryConsume(maxPerDay int, now time.Time) (bool, error) {
	if maxPerDay <= 0 {
		return true, nil
	}
	st, err := q.load()
	if err != nil {
		return false, err
	}
	day := now.UTC().Format("2006-01-02")
	if st.Date != day {
		st = ReviewQuota{Date: day, Count: 0}
	}
	if st.Count >= maxPerDay {
		return false, nil
	}
	st.Count++
	return true, q.save(st)
}

func (q *QuotaStore) load() (ReviewQuota, error) {
	if q.Path == "" {
		return ReviewQuota{}, fmt.Errorf("quota path is required")
	}
	data, err := os.ReadFile(q.Path)
	if err != nil {
		if os.IsNotExist(err) {
			return ReviewQuota{}, nil
		}
		return ReviewQuota{}, err
	}
	var st ReviewQuota
	if err := json.Unmarshal(data, &st); err != nil {
		return ReviewQuota{}, err
	}
	return st, nil
}

func (q *QuotaStore) save(st ReviewQuota) error {
	if err := os.MkdirAll(filepath.Dir(q.Path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	return configfile.WriteAtomic(q.Path, data, 0o644)
}
