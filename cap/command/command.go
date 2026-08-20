package command

import "context"

type Registry interface {
	Register(Descriptor, Handler) error
}

type Handler interface {
	Handle(context.Context, Request) (Result, error)
}

type Descriptor struct{}
type Request struct{}
type Result struct{}
