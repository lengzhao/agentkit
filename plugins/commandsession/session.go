package commandsession

import (
	"context"
	"fmt"
	"io"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/command"
	"github.com/lengzhao/agentkit/runtime/session"
	"github.com/lengzhao/pluginkit"
)

type Config struct{}

type Deps struct {
	SessionStore agentkit.SessionStore `json:"sessionStore"`
}

type Show struct {
	sessionStore agentkit.SessionStore
}

func init() {
	pluginkit.Register("command/session", New)
}

func New(_ Config, deps Deps) (command.Handler, error) {
	if deps.SessionStore == nil {
		return nil, fmt.Errorf("command/session requires sessionStore dependency")
	}
	return &Show{sessionStore: deps.SessionStore}, nil
}

func (s *Show) Descriptor() command.Descriptor {
	return command.Descriptor{
		Name:        "session",
		Description: "show current session id, path, and message count",
		Aliases:     []string{"sess"},
	}
}

func (s *Show) Handle(ctx context.Context, req command.Request) (command.Result, error) {
	if req.SessionID == "" {
		return command.Result{}, fmt.Errorf("session id is required")
	}
	sess, err := s.sessionStore.Get(ctx, req.SessionID)
	if err != nil {
		return command.Result{}, err
	}
	events, err := session.ReadAllEvents(ctx, sess)
	if err != nil {
		return command.Result{}, err
	}
	messages, err := sess.DeriveMessages(ctx)
	if err != nil {
		return command.Result{}, err
	}

	path := sessionPath(sess)
	writeLine(req.ErrOut, fmt.Sprintf("session id: %s", req.SessionID))
	writeLine(req.ErrOut, fmt.Sprintf("path: %s", path))
	writeLine(req.ErrOut, fmt.Sprintf("events: %d", len(events)))
	writeLine(req.ErrOut, fmt.Sprintf("messages: %d", len(messages)))
	return command.Result{}, nil
}

func sessionPath(sess agentkit.Session) string {
	if p, ok := sess.(session.FileBacked); ok {
		if path := p.FilePath(); path != "" {
			return path
		}
	}
	return "(memory)"
}

func writeLine(w io.Writer, line string) {
	if w == nil {
		return
	}
	fmt.Fprintln(w, line)
}

var _ command.Handler = (*Show)(nil)
