package filesystem

import "context"

type Service interface {
	ReadText(context.Context, string, int) (string, error)
	WriteText(context.Context, string, string) error
	Edit(context.Context, EditRequest) (EditResult, error)
}

type EditRequest struct{}
type EditResult struct{}
