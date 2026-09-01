package dreaming

import (
	"fmt"
	"math"
	"strings"
	"time"
)

type scored struct {
	Signal Signal
	Score  float64
	Reason string
}

func scoreSignals(signals []Signal, cfg Config, now time.Time) []scored {
	cfg = cfg.Normalized()
	halfLife := cfg.recencyHalfLife()
	out := make([]scored, 0, len(signals))
	for _, sig := range signals {
		score, reason := scoreOne(sig, cfg, now, halfLife)
		out = append(out, scored{Signal: sig, Score: score, Reason: reason})
	}
	return out
}

func scoreOne(sig Signal, cfg Config, now time.Time, halfLife time.Duration) (float64, string) {
	relevance := relevanceScore(sig.Text)
	frequency := math.Min(1, float64(sig.RecallCount)/float64(cfg.MinRecallCount*2))
	diversity := math.Min(1, float64(len(sig.SessionSources))/float64(cfg.MinUniqueSessions))
	recency := recencyScore(sig.LastSeen, now, halfLife)
	consolidation := math.Min(1, float64(daysBetween(sig.FirstSeen, now))/7)
	richness := math.Min(1, float64(len([]rune(sig.Text)))/120)

	total := 0.30*relevance +
		0.24*frequency +
		0.15*diversity +
		0.15*recency +
		0.10*consolidation +
		0.06*richness

	phaseBoost := math.Min(0.08, 0.02*float64(sig.LightHits+sig.REMHits))
	total += phaseBoost
	if total > 1 {
		total = 1
	}

	reason := strings.TrimSpace(strings.Join([]string{
		formatPart("relevance", relevance),
		formatPart("frequency", frequency),
		formatPart("diversity", diversity),
		formatPart("recency", recency),
	}, " "))
	return total, reason
}

func formatPart(name string, v float64) string {
	return fmt.Sprintf("%s=%.2f", name, v)
}

func relevanceScore(text string) float64 {
	lower := strings.ToLower(text)
	needles := []string{
		"prefer", "always", "never", "remember", "from now on",
		"偏好", "记住", "以后", "默认", "纠正", "不要",
	}
	hits := 0
	for _, n := range needles {
		if strings.Contains(lower, n) {
			hits++
		}
	}
	if hits == 0 {
		return 0.35
	}
	return math.Min(1, 0.5+float64(hits)*0.15)
}

func recencyScore(last time.Time, now time.Time, halfLife time.Duration) float64 {
	if last.IsZero() || halfLife <= 0 {
		return 0.5
	}
	age := now.Sub(last)
	if age < 0 {
		age = 0
	}
	return math.Pow(0.5, age.Seconds()/halfLife.Seconds())
}

func daysBetween(a, b time.Time) int {
	if a.IsZero() || b.IsZero() {
		return 0
	}
	d := b.Sub(a)
	if d < 0 {
		return 0
	}
	return int(d.Hours() / 24)
}

func passesThreshold(s scored, cfg Config) bool {
	cfg = cfg.Normalized()
	if s.Score < cfg.MinScore {
		return false
	}
	if s.Signal.RecallCount < cfg.MinRecallCount {
		return false
	}
	if len(s.Signal.SessionSources) < cfg.MinUniqueSessions {
		return false
	}
	return true
}
