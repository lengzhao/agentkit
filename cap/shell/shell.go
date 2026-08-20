package shell

import "context"

type Executor interface {
	Run(context.Context, Request) (Result, error)
}

type Request struct{}
type Result struct{}
