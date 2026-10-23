package common

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/lengzhao/agentkit/cap/interaction"
)

const DefaultCardSubmissionTTL = 24 * time.Hour

func ParseCardSubmissionTTL(value string) (time.Duration, error) {
	if strings.TrimSpace(value) == "" {
		return DefaultCardSubmissionTTL, nil
	}
	ttl, err := time.ParseDuration(value)
	if err != nil || ttl <= 0 {
		return 0, fmt.Errorf("cardSubmissionTTL must be a positive duration, got %q", value)
	}
	return ttl, nil
}

// CardSubmissions is independent of transport event deduplication. Plugins do
// not enter this gate; only final default-agent submissions do. Completed
// entries expire automatically; pending queue deliveries are never expired.
type CardSubmissions struct {
	mu      sync.Mutex
	entries map[string]*cardSubmission
}
type cardSubmission struct{ expires time.Time }

func cardSubmissionKey(e interaction.Event) string {
	if e.Purpose != interaction.PurposeSubmit || e.SubmissionMode == interaction.SubmissionRepeat {
		return ""
	}
	id := e.MessageID
	if id == "" {
		id = e.InteractionID
	}
	if id == "" {
		return ""
	}
	user := ""
	if e.SubmissionMode == interaction.SubmissionPerUser {
		user = e.UserID
		if user == "" {
			return ""
		}
	}
	b, _ := json.Marshal([]string{e.Platform, e.ChatID, id, user})
	return string(b)
}
func (s *CardSubmissions) lookup(key string) *cardSubmission {
	entry := s.entries[key]
	if entry != nil && !entry.expires.IsZero() && !time.Now().Before(entry.expires) {
		delete(s.entries, key)
		return nil
	}
	return entry
}
func (s *CardSubmissions) Contains(e interaction.Event) bool {
	key := cardSubmissionKey(e)
	if key == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lookup(key) != nil
}
func (s *CardSubmissions) Begin(e interaction.Event, ttl time.Duration) (func(error), bool) {
	key := cardSubmissionKey(e)
	if key == "" {
		return func(error) {}, false
	}
	if ttl <= 0 {
		ttl = DefaultCardSubmissionTTL
	}
	s.mu.Lock()
	if s.entries == nil {
		s.entries = make(map[string]*cardSubmission)
	}
	if s.lookup(key) != nil {
		s.mu.Unlock()
		return func(error) {}, true
	}
	entry := &cardSubmission{}
	s.entries[key] = entry
	s.mu.Unlock()
	var once sync.Once
	return func(err error) {
		once.Do(func() {
			s.mu.Lock()
			defer s.mu.Unlock()
			if err != nil {
				delete(s.entries, key)
				return
			}
			entry.expires = time.Now().Add(ttl)
			time.AfterFunc(ttl, func() {
				s.mu.Lock()
				defer s.mu.Unlock()
				if s.entries[key] == entry {
					delete(s.entries, key)
				}
			})
		})
	}, false
}

// ApplyInteractionPolicy reads optional author-supplied metadata. Adapters set
// safe native defaults first. Slack values can encode this object as JSON text.
func ApplyInteractionPolicy(e *interaction.Event, value any) {
	var m map[string]any
	switch x := value.(type) {
	case map[string]any:
		m = x
	case string:
		_ = json.Unmarshal([]byte(x), &m)
	}
	switch purpose, _ := m["purpose"].(string); interaction.Purpose(purpose) {
	case interaction.PurposeSubmit, interaction.PurposeChange, interaction.PurposeNavigate:
		e.Purpose = interaction.Purpose(purpose)
		if e.Purpose == interaction.PurposeSubmit {
			e.SubmissionMode = interaction.SubmissionOnce
		}
	}
	switch mode, _ := m["mode"].(string); interaction.SubmissionMode(mode) {
	case interaction.SubmissionOnce, interaction.SubmissionPerUser, interaction.SubmissionRepeat:
		e.SubmissionMode = interaction.SubmissionMode(mode)
	}
}

func SubmittedInteractionText(e interaction.Event) string {
	var b strings.Builder
	if e.CardText != "" {
		b.WriteString(e.CardText)
		b.WriteString("\n\n")
	}
	b.WriteString("已提交")
	for _, answer := range e.Answers {
		// Routing/button metadata belongs in RawAction, not in the answer summary.
		value := answer.Value
		if m, ok := value.(map[string]any); ok {
			value = nil
			for _, key := range []string{"answer", "option", "selected", "value"} {
				if v, exists := m[key]; exists {
					value = v
					break
				}
			}
			if value == nil && answer.Label == "" {
				payload := make(map[string]any)
				for k, v := range m {
					switch k {
					case "action", "session_key", "agentkit_interaction":
						continue
					}
					payload[k] = v
				}
				if len(payload) == 0 {
					continue
				}
				value = interactionValue(payload)
			}
		}
		b.WriteString("\n")
		if answer.Control != "" {
			b.WriteString(answer.Control)
			b.WriteString(": ")
		}
		if answer.Label != "" {
			b.WriteString(answer.Label)
			if value != nil {
				b.WriteString(" (")
				b.WriteString(fmt.Sprint(value))
				b.WriteString(")")
			}
		} else {
			b.WriteString(fmt.Sprint(value))
		}
	}
	return b.String()
}

// InteractionSubmissionSummary excludes the original content already present on the card.
func InteractionSubmissionSummary(e interaction.Event) string {
	e.CardText = ""
	return SubmittedInteractionText(e)
}

type interactionCardResponder struct {
	close func(context.Context, interaction.Event, string) error
	event interaction.Event
}

func (r interactionCardResponder) CloseCard(ctx context.Context, text string) error {
	if text == "" {
		text = InteractionSubmissionSummary(r.event)
	}
	return r.close(ctx, r.event, text)
}

// InteractionControlInForm recognizes native form/input ancestors.
func InteractionControlInForm(raw json.RawMessage, control string) bool {
	inside, _ := InteractionControlForm(raw, control)
	return inside
}

// InteractionControlForm also returns a native button's form_action_type.
// Explicit interaction policy remains authoritative over native semantics.
func InteractionControlForm(raw json.RawMessage, control string) (bool, string) {
	if control == "" {
		return false, ""
	}
	var root any
	if json.Unmarshal(raw, &root) != nil {
		return false, ""
	}
	var walk func(any, bool) (bool, string)
	walk = func(v any, inForm bool) (bool, string) {
		switch x := v.(type) {
		case []any:
			for _, item := range x {
				if found, action := walk(item, inForm); found {
					return true, action
				}
			}
		case map[string]any:
			inForm = inForm || x["tag"] == "form" || x["type"] == "input"
			if (x["name"] == control || x["action_id"] == control) && inForm {
				action, _ := x["form_action_type"].(string)
				return true, action
			}
			for _, child := range x {
				if found, action := walk(child, inForm); found {
					return true, action
				}
			}
		}
		return false, ""
	}
	return walk(root, false)
}
