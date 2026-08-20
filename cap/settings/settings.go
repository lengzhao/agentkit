package settings

import "context"

type Store interface {
	Get(context.Context, string) (Value, error)
}

type Value struct{}
