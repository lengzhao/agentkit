package acpplatform

import (
	"context"
	"sync"

	acp "github.com/coder/acp-go-sdk"
	"github.com/lengzhao/agentkit"
)

type turnResult struct {
	stopReason acp.StopReason
	err        error
}

type sessionState struct {
	acpSessionID acp.SessionId
	deliveryID   agentkit.SessionID
	cwd          string

	turnMu sync.Mutex
	turnCh chan turnResult

	promptMu     sync.Mutex
	promptCancel context.CancelFunc

	cancelTurnMu sync.Mutex
	cancelTurn   func()
}

func (s *sessionState) setTurnWait(ch chan turnResult) {
	s.turnMu.Lock()
	s.turnCh = ch
	s.turnMu.Unlock()
}

func (s *sessionState) completeTurn(result turnResult) {
	s.turnMu.Lock()
	ch := s.turnCh
	s.turnCh = nil
	s.turnMu.Unlock()
	if ch != nil {
		select {
		case ch <- result:
		default:
		}
	}
}

func (s *sessionState) setPromptCancel(cancel context.CancelFunc) {
	s.promptMu.Lock()
	if s.promptCancel != nil {
		s.promptCancel()
	}
	s.promptCancel = cancel
	s.promptMu.Unlock()
}

func (s *sessionState) clearPromptCancel() {
	s.promptMu.Lock()
	s.promptCancel = nil
	s.promptMu.Unlock()
}

func (s *sessionState) setCancelTurn(fn func()) {
	s.cancelTurnMu.Lock()
	s.cancelTurn = fn
	s.cancelTurnMu.Unlock()
}

func (s *sessionState) invokeCancelTurn() {
	s.cancelTurnMu.Lock()
	fn := s.cancelTurn
	s.cancelTurn = nil
	s.cancelTurnMu.Unlock()
	if fn != nil {
		fn()
	}
}
