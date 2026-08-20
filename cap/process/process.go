package process

import "context"

type Service interface {
	Spawn(context.Context, Request) (Handle, error)
}

type Request struct{}
type Handle interface{}
