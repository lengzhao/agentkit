package dreaming

import "time"

// Config controls grounded dreaming sweeps.
type Config struct {
	Enabled             *bool   `json:"enabled"`
	Frequency           string  `json:"frequency"` // cron, default "0 3 * * *"
	// FeedReview when true, top grounded signals are appended to background-review digest.
	FeedReview *bool `json:"feedReview"`
	FeedReviewTopK int `json:"feedReviewTopK"`
	MinScore            float64 `json:"minScore"`
	MinRecallCount      int     `json:"minRecallCount"`
	MinUniqueSessions   int     `json:"minUniqueSessions"`
	RecencyHalfLifeDays int     `json:"recencyHalfLifeDays"`
	MaxPromotedChars    int     `json:"maxPromotedChars"`
	SessionScanLimit    int     `json:"sessionScanLimit"`
}

// Defaults returns OpenClaw-aligned dreaming defaults.
func Defaults() Config {
	enabled := true
	return Config{
		Enabled:             &enabled,
		Frequency:           "0 3 * * *",
		MinScore:            0.75,
		MinRecallCount:      3,
		MinUniqueSessions:   2,
		RecencyHalfLifeDays: 14,
		MaxPromotedChars:    400,
		SessionScanLimit:    32,
	}
}

// IsEnabled reports whether dreaming sweeps are allowed by config.
func (c Config) IsEnabled() bool {
	return c.enabledFlag()
}

// FeedReviewEnabled reports whether scored signals are shown to background review.
func (c Config) FeedReviewEnabled() bool {
	if c.FeedReview == nil {
		return true
	}
	return *c.FeedReview
}

func (c Config) enabledFlag() bool {
	if c.Enabled == nil {
		return Defaults().enabledFlag()
	}
	return *c.Enabled
}

func (c Config) Normalized() Config {
	d := Defaults()
	out := d
	if c.Enabled != nil {
		out.Enabled = c.Enabled
	}
	if c.Frequency != "" {
		out.Frequency = c.Frequency
	}
	if c.MinScore > 0 {
		out.MinScore = c.MinScore
	}
	if c.MinRecallCount > 0 {
		out.MinRecallCount = c.MinRecallCount
	}
	if c.MinUniqueSessions > 0 {
		out.MinUniqueSessions = c.MinUniqueSessions
	}
	if c.RecencyHalfLifeDays > 0 {
		out.RecencyHalfLifeDays = c.RecencyHalfLifeDays
	}
	if c.MaxPromotedChars > 0 {
		out.MaxPromotedChars = c.MaxPromotedChars
	}
	if c.SessionScanLimit > 0 {
		out.SessionScanLimit = c.SessionScanLimit
	}
	if c.FeedReview != nil {
		out.FeedReview = c.FeedReview
	}
	if c.FeedReviewTopK > 0 {
		out.FeedReviewTopK = c.FeedReviewTopK
	}
	return out
}

func (c Config) recencyHalfLife() time.Duration {
	return time.Duration(c.Normalized().RecencyHalfLifeDays) * 24 * time.Hour
}
