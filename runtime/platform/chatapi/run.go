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

var (
	errInteractionExpired = errors.New("interaction expired")
	errRunAlreadyAttached = errors.New("run already attached")
)

type interactionState struct {
	ID        string
	Prompt    string
	Options   []string
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
	channelKey     string
	agentID        agentkit.AgentID
	sessionID      agentkit.SessionID
	conversationID string
	messageID      string
	apiBase        string

	platform *Platform

	mu                   sync.Mutex
	flushMu              sync.Mutex
	answerText           string
	sentAnswer           string
	thinkingText         string
	sentThinking         string
	interaction          *interactionState
	finishTimer          *time.Timer
	interactionTimer     *time.Timer
	sink                 runEventSink
	detached             bool
	attaching            bool
	lastRecoverableEvent *recoverableEvent

	notify chan struct{}
	done   chan pendingResult
	once   sync.Once
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

func (s *pendingStore) finish(id string, result pendingResult) bool {
	run := s.get(id)
	if run == nil {
		return false
	}
	run.mu.Lock()
	if result.answer == "" {
		result.answer = run.answerText
	}
	run.lastRecoverableEvent = nil
	run.mu.Unlock()
	if !run.complete(result) {
		return false
	}
	s.delete(id)
	return true
}

func (s *pendingStore) cancelUser(id string) bool {
	run := s.get(id)
	if run == nil {
		return false
	}
	if !run.complete(pendingResult{err: errUserCanceled}) {
		return false
	}
	s.delete(id)
	return true
}

func newRunState(id, user, channelKey string, agentID agentkit.AgentID, sessionID agentkit.SessionID, conversationID, messageID string, p *Platform, sse *sseWriter) *runState {
	r := &runState{
		id:             id,
		user:           user,
		channelKey:     channelKey,
		agentID:        agentID,
		sessionID:      sessionID,
		conversationID: conversationID,
		messageID:      messageID,
		platform:       p,
		notify:         make(chan struct{}, 1),
		done:           make(chan pendingResult, 1),
	}
	if sse != nil {
		r.sink = &sseEventSink{w: sse}
	}
	return r
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

func (r *runState) complete(result pendingResult) bool {
	var ok bool
	r.once.Do(func() {
		r.stopTimers()
		r.done <- result
		ok = true
	})
	return ok
}

func (r *runState) signal() {
	select {
	case r.notify <- struct{}{}:
	default:
	}
	r.mu.Lock()
	detached := r.detached
	r.mu.Unlock()
	if detached {
		_ = r.flushDelta()
	}
}

func (r *runState) detach() {
	r.flushMu.Lock()
	defer r.flushMu.Unlock()
	r.detachUnderFlush()
}

func (r *runState) detachUnderFlush() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.detached {
		return
	}
	r.detached = true
	r.sink = &detachedEventSink{run: r}
	if r.lastRecoverableEvent == nil {
		r.lastRecoverableEvent = &recoverableEvent{
			name: "ping",
			payload: map[string]any{
				"run_id": r.id,
				"ts":     time.Now().Unix(),
			},
			createdAt: time.Now(),
		}
	}
}

func (r *runState) beginAttach() error {
	r.flushMu.Lock()
	defer r.flushMu.Unlock()
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.attaching || (r.sink != nil && r.sink.Active()) {
		return errRunAlreadyAttached
	}
	r.attaching = true
	return nil
}

func (r *runState) finishAttach(sse *sseWriter) {
	r.flushMu.Lock()
	defer r.flushMu.Unlock()
	r.mu.Lock()
	defer r.mu.Unlock()
	r.detached = false
	r.attaching = false
	r.sink = &sseEventSink{w: sse}
}

func (r *runState) cancelAttach() {
	r.mu.Lock()
	r.attaching = false
	r.mu.Unlock()
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
		r.platform.pending.finish(r.id, pendingResult{answer: r.finalAnswer()})
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
	r.signal()
}

func (r *runState) appendThinking(delta string) {
	if delta == "" {
		return
	}
	r.mu.Lock()
	r.thinkingText += delta
	r.mu.Unlock()
	r.signal()
}

func (r *runState) flushDelta() error {
	r.flushMu.Lock()
	defer r.flushMu.Unlock()
	if err := r.flushThinkingDelta(); err != nil {
		return err
	}
	return r.flushAnswerDelta()
}

func (r *runState) flushThinkingDelta() error {
	r.mu.Lock()
	sink := r.sink
	messageID := r.messageID
	curr := r.thinkingText
	prev := r.sentThinking
	active := sink != nil && sink.Active()
	r.mu.Unlock()
	if sink == nil {
		return nil
	}
	if !active {
		if curr == "" || curr == prev {
			return nil
		}
		r.mu.Lock()
		hasAnswer := strings.TrimSpace(r.answerText) != ""
		r.mu.Unlock()
		if hasAnswer {
			return nil
		}
		payload := replaceDeltaPayload(messageID, curr)
		if err := sink.Event("thinking_delta", payload); err != nil {
			return err
		}
		r.mu.Lock()
		r.sentThinking = curr
		r.mu.Unlock()
		return nil
	}

	payload, ok := deltaPayload(messageID, prev, curr)
	if !ok {
		return nil
	}
	if err := sink.Event("thinking_delta", payload); err != nil {
		r.detachUnderFlush()
		return err
	}
	r.mu.Lock()
	r.sentThinking = curr
	r.mu.Unlock()
	return nil
}

func (r *runState) flushAnswerDelta() error {
	r.mu.Lock()
	sink := r.sink
	messageID := r.messageID
	curr := r.answerText
	prev := r.sentAnswer
	active := sink != nil && sink.Active()
	r.mu.Unlock()
	if sink == nil {
		return nil
	}
	if !active {
		if curr == "" || curr == prev {
			return nil
		}
		payload := replaceDeltaPayload(messageID, curr)
		if err := sink.Event("text_delta", payload); err != nil {
			return err
		}
		r.mu.Lock()
		r.sentAnswer = curr
		r.mu.Unlock()
		return nil
	}

	payload, ok := deltaPayload(messageID, prev, curr)
	if !ok {
		return nil
	}
	if err := sink.Event("text_delta", payload); err != nil {
		r.detachUnderFlush()
		return err
	}
	r.mu.Lock()
	r.sentAnswer = curr
	r.mu.Unlock()
	return nil
}

func (r *runState) emitSSE(name string, payload any) error {
	r.mu.Lock()
	sink := r.sink
	r.mu.Unlock()
	if sink == nil {
		return nil
	}
	if err := sink.Event(name, payload); err != nil {
		r.detachUnderFlush()
		return err
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
	return replaceDeltaPayload(messageID, curr), true
}

func replaceDeltaPayload(messageID, curr string) map[string]any {
	return map[string]any{"message_id": messageID, "text": curr, "replace": true}
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
