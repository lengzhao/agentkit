package learning

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/lengzhao/agentkit/cap/filesystem"
)

// ReviewQuota tracks per-day background review runs for throttling.
type ReviewQuota struct {
	Date  string `json:"date"`
	Count int    `json:"count"`
}

// QuotaStore persists review_quota.json under memory/dreaming/.
// Path is a filesystem.Service-relative path.
type QuotaStore struct {
	FS   filesystem.Service
	Path string
}

func (q *QuotaStore) TryConsume(ctx context.Context, maxPerDay int, now time.Time) (bool, error) {
	if maxPerDay <= 0 {
		return true, nil
	}
	st, err := q.load(ctx)
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
	return true, q.save(ctx, st)
}

func (q *QuotaStore) load(ctx context.Context) (ReviewQuota, error) {
	if q.Path == "" {
		return ReviewQuota{}, fmt.Errorf("quota path is required")
	}
	data, err := q.FS.Read(ctx, q.Path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
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

func (q *QuotaStore) save(ctx context.Context, st ReviewQuota) error {
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	return q.FS.Write(ctx, q.Path, data)
}
