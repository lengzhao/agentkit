package ask

import (
	"context"

	"github.com/lengzhao/agentkit/cap/ask"
)

type UnavailableConfig struct {
	// Reason is handed back to the model verbatim. Say what it should do
	// instead, because it cannot ask anyone.
	Reason string `json:"reason,omitempty"`
}

// Unavailable answers every question with "nobody is here". It is the provider
// for unattended platforms (worker, timer, cron), where blocking on a human is
// the same as hanging.
type Unavailable struct{ reason string }

func NewUnavailable(cfg UnavailableConfig) (ask.Service, error) {
	reason := cfg.Reason
	if reason == "" {
		reason = "this run is unattended, no user can answer"
	}
	return &Unavailable{reason: reason}, nil
}

func (u *Unavailable) Ask(_ context.Context, _ ask.Question) (ask.Answer, error) {
	return ask.Answer{Selected: -1, Reason: u.reason}, nil
}
