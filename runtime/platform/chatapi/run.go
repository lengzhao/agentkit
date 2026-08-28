package chatapi

import (
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/lengzhao/agentkit"
)

const (
	defaultMaxRuns            = 1000
	defaultInteractionTimeout = 10 * time.Minute
	finishDebounce            = 400 * time.Millisecond
)

var errInteractionExpired = errors.New("interaction expired")

type interactionState struct {
	ID       string
	Prompt   string
	Options  []string
	ExpiresAt time.Time
	Responded bool
}

type pendingResult struct {
	err    error
	answer string
}

type runState struct {
	id             string
	user           string
	userQuery      string
	channelKey     string
	agentID        agentkit.AgentID
	sessionID      agentkit.SessionID
	conversationID string
	messageID      string

	platform *Platform
	sse      *sseWriter

	mu              sync.Mutex
	answerText      string
	sentAnswer      string
	thinkingText    string
	sentThinking    string
	interaction     *interactionState
	finishTimer     *time.Timer
	interactionTimer *time.Timer

	done chan pendingResult
	once sync.Once
}

type pendingStore struct {
	mu   sync.Mutex
	runs map[string]*runState
	max  int
}

func newPendingStore(max int) *pendingStore {
	if max <= 0 {
		max = defaultMaxRuns
	}
	return &pendingStore{runs: make(map[string]*runState), max: max}
}

func (s *pendingStore) create(run *runState) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.runs) >= s.max {
		return false
	}
	s.runs[run.id] = run
	return true
}

func (s *pendingStore) get(id string) *runState {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.runs[id]
}

func (s *pendingStore) delete(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if run := s.runs[id]; run != nil {
		run.stopTimers()
	}
	delete(s.runs, id)
}

func (s *pendingStore) finish(id string, result pendingResult) {
	run := s.get(id)
	if run == nil {
		return
	}
	run.complete(result)
	s.delete(id)
}

func newRunState(id, user, channelKey string, agentID agentkit.AgentID, sessionID agentkit.SessionID, conversationID, messageID string, p *Platform, sse *sseWriter) *runState {
	return &runState{
		id:             id,
		user:           user,
		channelKey:     channelKey,
		agentID:        agentID,
		sessionID:      sessionID,
		conversationID: conversationID,
		messageID:      messageID,
		platform:       p,
		sse:            sse,
		done:           make(chan pendingResult, 1),
	}
}

func (r *runState) stopTimers() {
	r.mu.Lock()
	if r.finishTimer != nil {
		r.finishTimer.Stop()
		r.finishTimer = nil
	}
	if r.interactionTimer != nil {
		r.interactionTimer.Stop()
		r.interactionTimer = nil
	}
	r.mu.Unlock()
}

func (r *runState) complete(result pendingResult) {
	r.once.Do(func() {
		r.stopTimers()
		r.platform.emitTerminalSSE(r, result)
		r.done <- result
	})
}

func (r *runState) scheduleFinish() {
	r.mu.Lock()
	if r.interaction != nil && !r.interaction.Responded {
		r.mu.Unlock()
		return
	}
	if r.finishTimer != nil {
		r.finishTimer.Stop()
	}
	r.finishTimer = time.AfterFunc(finishDebounce, func() {
		r.complete(pendingResult{answer: r.finalAnswer()})
	})
	r.mu.Unlock()
}

func (r *runState) cancelFinish() {
	r.mu.Lock()
	if r.finishTimer != nil {
		r.finishTimer.Stop()
		r.finishTimer = nil
	}
	r.mu.Unlock()
}

func (r *runState) finalAnswer() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.answerText
}

func (r *runState) appendAnswer(delta string) {
	if delta == "" {
		return
	}
	r.mu.Lock()
	r.answerText += delta
	r.mu.Unlock()
}

func (r *runState) appendThinking(delta string) {
	if delta == "" {
		return
	}
	r.mu.Lock()
	r.thinkingText += delta
	r.mu.Unlock()
}

func (r *runState) flushDeltas() error {
	if r.sse == nil {
		return nil
	}
	r.mu.Lock()
	thinking := r.thinkingText
	prevThinking := r.sentThinking
	answer := r.answerText
	prevAnswer := r.sentAnswer
	msgID := r.messageID
	r.mu.Unlock()

	if thinking != prevThinking {
		payload, ok := deltaPayload(msgID, prevThinking, thinking)
		if ok {
			if err := r.sse.Event("thinking_delta", payload); err != nil {
				return err
			}
			r.mu.Lock()
			r.sentThinking = thinking
			r.mu.Unlock()
		}
	}
	if answer != prevAnswer {
		payload, ok := deltaPayload(msgID, prevAnswer, answer)
		if ok {
			if err := r.sse.Event("text_delta", payload); err != nil {
				return err
			}
			r.mu.Lock()
			r.sentAnswer = answer
			r.mu.Unlock()
		}
	}
	return nil
}

func deltaPayload(messageID, prev, curr string) (map[string]any, bool) {
	if strings.HasPrefix(curr, prev) {
		suffix := curr[len(prev):]
		if suffix == "" {
			return nil, false
		}
		return map[string]any{"message_id": messageID, "text": suffix}, true
	}
	return map[string]any{"message_id": messageID, "text": curr, "replace": true}, true
}

func (r *runState) setInteraction(ix *interactionState) {
	r.mu.Lock()
	if r.interactionTimer != nil {
		r.interactionTimer.Stop()
	}
	r.interaction = ix
	if ix != nil {
		delay := time.Until(ix.ExpiresAt)
		if delay < time.Second {
			delay = time.Second
		}
		r.interactionTimer = time.AfterFunc(delay, func() {
			r.platform.onInteractionTimeout(r.id, ix.ID)
		})
	}
	r.mu.Unlock()
	r.cancelFinish()
}

func (r *runState) markInteractionResponded(id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.interaction == nil || r.interaction.ID != id {
		return errors.New("not found")
	}
	if r.interaction.Responded {
		return errors.New("already responded")
	}
	r.interaction.Responded = true
	if r.interactionTimer != nil {
		r.interactionTimer.Stop()
		r.interactionTimer = nil
	}
	return nil
}

func (r *runState) getInteraction(id string) *interactionState {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.interaction == nil || r.interaction.ID != id {
		return nil
	}
	cp := *r.interaction
	return &cp
}
