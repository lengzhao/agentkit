package dreaming

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/lengzhao/agentkit/cap/filesystem"
)

// SweepResult summarizes one dreaming run.
type SweepResult struct {
	SessionsIngested int
	SignalsIngested  int
	Staged           int
	Themes           []string
	// Eligible is signals above Deep threshold (background review consolidates).
	Eligible int
	Skipped  int
}

// Run executes Light → REM → Deep and appends Dream Diary blocks.
// deepReportDir and sessionsDir are filesystem.Service-relative (via stateStore.FS).
func Run(ctx context.Context, cfg Config, stateStore *Store, diary *Diary, deepReportDir string, sessionsDir string, now time.Time) (*SweepResult, error) {
	cfg = cfg.Normalized()
	st, err := stateStore.Load(ctx)
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
		sc, sig, err := IngestSessions(ctx, stateStore.FS, sessionsDir, stateStore, cfg.SessionScanLimit, now)
		if err != nil {
			return nil, err
		}
		res.SessionsIngested = sc
		res.SignalsIngested = sig
		st, _ = stateStore.Load(ctx)
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
		if err := diary.AppendPhase(ctx, "Light Sleep", now, lightLines); err != nil {
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
		if err := diary.AppendPhase(ctx, "REM Sleep", now, remLines); err != nil {
			return res, err
		}
	}

	// Deep — score only; memory.md is written by background review.
	scored := scoreSignals(st.Signals, cfg, now)
	sort.Slice(scored, func(i, j int) bool { return scored[i].Score > scored[j].Score })

	eligible := 0
	skipped := 0
	for _, item := range scored {
		if !passesThreshold(item, cfg) {
			skipped++
			continue
		}
		eligible++
	}
	res.Eligible = eligible
	res.Skipped = skipped
	res.Staged = staged

	st.LastSweep = now
	if err := stateStore.Save(ctx, st); err != nil {
		return res, err
	}

	deepLines := []string{
		fmt.Sprintf("%d grounded candidates above threshold (background review consolidates)", eligible),
		fmt.Sprintf("skipped %d below threshold", skipped),
	}
	if diary != nil {
		if err := diary.AppendPhase(ctx, "Deep Sleep", now, deepLines); err != nil {
			return res, err
		}
	}
	if deepReportDir != "" {
		if err := writeDeepReport(ctx, stateStore.FS, deepReportDir, now, eligibleTexts(scored, cfg), skipped); err != nil {
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

func eligibleTexts(scored []scored, cfg Config) []string {
	out := []string{}
	for _, item := range scored {
		if !passesThreshold(item, cfg) {
			continue
		}
		text := strings.TrimSpace(item.Signal.Text)
		if text == "" {
			continue
		}
		if len([]rune(text)) > cfg.MaxPromotedChars {
			text = string([]rune(text)[:cfg.MaxPromotedChars]) + "…"
		}
		out = append(out, text)
	}
	return out
}

func writeDeepReport(ctx context.Context, fs filesystem.Service, dir string, now time.Time, lines []string, skipped int) error {
	path := strings.TrimSuffix(dir, "/") + "/" + now.UTC().Format("2006-01-02") + ".md"
	var b strings.Builder
	b.WriteString("# Deep Sleep Report\n\n")
	b.WriteString(fmt.Sprintf("eligible for review: %d\nskipped: %d\n\n", len(lines), skipped))
	for _, p := range lines {
		b.WriteString("- ")
		b.WriteString(p)
		b.WriteByte('\n')
	}
	return fs.Write(ctx, path, []byte(b.String()))
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
	} else {
		b.WriteString("last sweep: never\n")
	}
	fmt.Fprintf(&b, "thresholds: score>=%.2f recall>=%d sessions>=%d\n",
		cfg.MinScore, cfg.MinRecallCount, cfg.MinUniqueSessions)
	fmt.Fprintf(&b, "memory consolidation: background review (dreaming does not write memory.md)\n")
	return strings.TrimRight(b.String(), "\n")
}
