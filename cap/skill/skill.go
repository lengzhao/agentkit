package skill

import "context"

type Registry interface {
	List(context.Context) ([]Descriptor, error)
	Load(context.Context, string) (Content, error)
}

type Descriptor struct{}
type Content struct{}
