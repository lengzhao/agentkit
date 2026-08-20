package compaction

import "context"

type Service interface {
	Compact(context.Context, Request) (Result, error)
}

type Request struct{}
type Result struct{}
