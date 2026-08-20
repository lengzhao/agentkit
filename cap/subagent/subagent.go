package subagent

import "context"

type Spawner interface {
	Start(context.Context, Request) (Handle, error)
}

type Request struct{}
type Handle interface{}
