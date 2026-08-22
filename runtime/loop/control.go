package loop

import (
	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/sessioncontrol"
)

func (l *Default) controlFor(sessionID agentkit.SessionID) *sessioncontrol.Control {
	v, _ := l.sessionControls.LoadOrStore(sessionID, sessioncontrol.New())
	return v.(*sessioncontrol.Control)
}
