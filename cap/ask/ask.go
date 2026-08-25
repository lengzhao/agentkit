// Package ask is the capability boundary for the agent asking the human a
// question mid-turn.
//
// This is deliberately not cap/approval: approval answers "may I run this tool
// call" with a bool, and the unattended presets wire approval/auto-allow, which
// would silently answer every question "yes". A question needs its own seam so
// that "nobody is here to answer" is a first-class, non-fatal outcome.
package ask

import "context"

// Service asks the human a question and returns their answer.
//
// Implementations must not return an error just because no human is reachable —
// that is Answer{Answered: false} plus a Reason, so the agent can decide for
// itself instead of failing the turn.
type Service interface {
	Ask(context.Context, Question) (Answer, error)
}

type Question struct {
	// Question is the text shown to the human.
	Question string `json:"question"`
	// Options, when non-empty, restricts the answer to one of these choices.
	Options []string `json:"options,omitempty"`
	// Default is used when the human answers with an empty line.
	Default string `json:"default,omitempty"`
}

type Answer struct {
	// Answered is false when no human was available or they declined to answer.
	Answered bool `json:"answered"`
	// Text is the answer; for Options it is the chosen option.
	Text string `json:"text,omitempty"`
	// Selected is the index into Question.Options, or -1 for free-form answers.
	Selected int `json:"selected"`
	// Reason explains an unanswered question, e.g. "no interactive user".
	Reason string `json:"reason,omitempty"`
}
