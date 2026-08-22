package commandnew

import (
	"context"
	"fmt"
	"io"

	"github.com/lengzhao/agentkit/cap/command"
	"github.com/lengzhao/agentkit/runtime/session"
	"github.com/lengzhao/pluginkit"
)

type Config struct{}

type Deps struct{}

type New struct{}

func init() {
	pluginkit.Register("command/new", NewHandler)
}

func NewHandler(_ Config, _ Deps) (command.Handler, error) {
	return &New{}, nil
}

func (n *New) Descriptor() command.Descriptor {
	return command.Descriptor{
		Name:        "new",
		Description: "start a new CLI session",
	}
}

func (n *New) Handle(_ context.Context, req command.Request) (command.Result, error) {
	id := session.NewCLISessionID()
	writeLine(req.ErrOut, fmt.Sprintf("new session: %s", id))
	return command.Result{NewSession: id}, nil
}

func writeLine(w io.Writer, line string) {
	if w == nil {
		return
	}
	fmt.Fprintln(w, line)
}

var _ command.Handler = (*New)(nil)
