package sessionquery

import "context"

type Service interface {
	Query(context.Context, Request) (Result, error)
}

type Request struct{}
type Result struct{}
