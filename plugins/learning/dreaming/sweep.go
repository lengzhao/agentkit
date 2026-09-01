package dreaming

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// PromoteFunc writes one promoted memory entry.
type PromoteFunc func(text, meta string) error

// SweepResult summarizes one dreaming run.
type SweepResult struct {
	SessionsIngested int
	SignalsIngested  int
	Staged           int
	Themes           []string
	Promoted         []string
	Skipped          int
}

// Run executes Light → REM → Deep and appends Dream Diary blocks.
func Run(cfg Config, stateStore *Store, diary *Diary, deepReportDir string, promote PromoteFunc, sessionsDir string, now time.Time) (*SweepResult, error) {
	cfg = cfg.Normalized()
	st, err := stateStore.Load()
	if err != nil {
		return nil, err
	}
	if !st.Enabled {
		return nil, fmt.Errorf("dreaming is disabled; use /learn dream on")
	}
	if !cfg.IsEnabled() {
		return nil, fmt.Errorf("dreaming is disabled in config")
	}

	res := &SweepResult{}
	if sessionsDir != "" {
		sc, sig, err := IngestSessions(sessionsDir, stateStore, cfg.SessionScanLimit, now)
		if err != nil {
			return nil, err
		}
		res.SessionsIngested = sc
		res.SignalsIngested = sig
		st, _ = stateStore.Load()
	}

	// Light
	staged := 0
	for i := range st.Signals {
		st.Signals[i].LightHits++
		staged++
	}
	lightLines := []string{
		fmt.Sprintf("ingested %d sessions, %d new signals", res.SessionsIngested, res.SignalsIngested),
		fmt.Sprintf("staged %d candidates", staged),
	}
	if diary != nil {
		if err := diary.AppendPhase("Light Sleep", now, lightLines); err != nil {
			return res, err
		}
	}

	// REM — theme buckets
	themes := remThemes(st.Signals)
	res.Themes = themes
	for i := range st.Signals {
		st.Signals[i].REMHits++
	}
	remLines := []string{"themes: " + strings.Join(themes, ", ")}
	if len(themes) == 0 {
		remLines = []string{"no strong themes this sweep"}
	}
	if diary != nil {
		if err := diary.AppendPhase("REM Sleep", now, remLines); err != nil {
			return res, err
		}
	}

	// Deep
	scored := scoreSignals(st.Signals, cfg, now)
	sort.Slice(scored, func(i, j int) bool { return scored[i].Score > scored[j].Score })

	promoted := []string{}
	skipped := 0
	today := now.UTC().Format("2006-01-02")
	if st.PromotedDate != today {
		st.PromotedDate = today
		st.PromotedToday = 0
	}
	for _, item := range scored {
		if !passesThreshold(item, cfg) {
			skipped++
			continue
		}
		text := strings.TrimSpace(item.Signal.Text)
		if len([]rune(text)) > cfg.MaxPromotedChars {
			text = string([]rune(text)[:cfg.MaxPromotedChars]) + "…"
		}
		meta := fmt.Sprintf("source=dream-deep score=%.2f %s promoted_at=%s",
			item.Score, item.Reason, now.UTC().Format(time.RFC3339))
		if promote != nil {
			if err := promote(text, meta); err != nil {
				skipped++
				continue
			}
		}
		promoted = append(promoted, text)
		st.PromotedToday++
	}
	res.Promoted = promoted
	res.Skipped = skipped
	res.Staged = staged

	// remove promoted signals from short-term store
	if len(promoted) > 0 {
		promotedKeys := map[string]struct{}{}
		for _, p := range promoted {
			promotedKeys[signalKey(p)] = struct{}{}
		}
		kept := make([]Signal, 0, len(st.Signals))
		for _, sig := range st.Signals {
			if _, ok := promotedKeys[signalKey(sig.Text)]; ok {
				continue
			}
			kept = append(kept, sig)
		}
		st.Signals = kept
	}

	st.LastSweep = now
	if err := stateStore.Save(st); err != nil {
		return res, err
	}

	deepLines := []string{
		fmt.Sprintf("promoted %d entries to memory.md", len(promoted)),
		fmt.Sprintf("skipped %d below threshold", skipped),
	}
	if diary != nil {
		if err := diary.AppendPhase("Deep Sleep", now, deepLines); err != nil {
			return res, err
		}
	}
	if deepReportDir != "" {
		if err := writeDeepReport(deepReportDir, now, promoted, skipped, scored); err != nil {
			return res, err
		}
	}
	return res, nil
}

func remThemes(signals []Signal) []string {
	type bucket struct {
		name  string
		count int
	}
	buckets := map[string]int{}
	for _, sig := range signals {
		for _, theme := range extractThemes(sig.Text) {
			buckets[theme]++
		}
	}
	list := make([]bucket, 0, len(buckets))
	for name, count := range buckets {
		list = append(list, bucket{name: name, count: count})
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].count == list[j].count {
			return list[i].name < list[j].name
		}
		return list[i].count > list[j].count
	})
	out := make([]string, 0, 3)
	for i, b := range list {
		if i >= 3 {
			break
		}
		out = append(out, fmt.Sprintf("%s (%d)", b.name, b.count))
	}
	return out
}

func extractThemes(text string) []string {
	lower := strings.ToLower(text)
	words := strings.FieldsFunc(lower, func(r rune) bool {
		return r <= ' ' || r == ',' || r == '.' || r == ':' || r == ';'
	})
	stop := map[string]struct{}{
		"the": {}, "and": {}, "for": {}, "that": {}, "with": {}, "from": {},
		"是": {}, "的": {}, "了": {}, "在": {}, "我": {}, "要": {},
	}
	themes := []string{}
	for _, w := range words {
		if len(w) < 4 {
			continue
		}
		if _, ok := stop[w]; ok {
			continue
		}
		themes = append(themes, w)
		if len(themes) >= 3 {
			break
		}
	}
	return themes
}

func writeDeepReport(dir string, now time.Time, promoted []string, skipped int, scored []scored) error {
	path := filepath.Join(dir, now.UTC().Format("2006-01-02")+".md")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	var b strings.Builder
	b.WriteString("# Deep Sleep Report\n\n")
	b.WriteString(fmt.Sprintf("promoted: %d\nskipped: %d\n\n", len(promoted), skipped))
	for _, p := range promoted {
		b.WriteString("- ")
		b.WriteString(p)
		b.WriteByte('\n')
	}
	return os.WriteFile(path, []byte(b.String()), 0o644)
}

// FormatStatus renders dreaming state for /learn dream status.
func FormatStatus(st *State, cfg Config) string {
	cfg = cfg.Normalized()
	var b strings.Builder
	enabled := "on"
	if st != nil && !st.Enabled {
		enabled = "off"
	}
	fmt.Fprintf(&b, "dreaming: %s\n", enabled)
	fmt.Fprintf(&b, "frequency: %s\n", cfg.Frequency)
	if st != nil && !st.LastSweep.IsZero() {
		fmt.Fprintf(&b, "last sweep: %s\n", st.LastSweep.UTC().Format(time.RFC3339))
		fmt.Fprintf(&b, "short-term signals: %d\n", len(st.Signals))
		fmt.Fprintf(&b, "promoted today: %d\n", st.PromotedToday)
	} else {
		b.WriteString("last sweep: never\n")
	}
	fmt.Fprintf(&b, "thresholds: score>=%.2f recall>=%d sessions>=%d\n",
		cfg.MinScore, cfg.MinRecallCount, cfg.MinUniqueSessions)
	return strings.TrimRight(b.String(), "\n")
}
