package credentials

import "context"

type Store interface {
	Resolve(context.Context, string) (Secret, error)
}

type Secret struct {
	Value string
	Ref   string
}
