package filesystem

import "context"

type Service interface {
	ReadText(context.Context, string, int) (string, error)
	WriteText(context.Context, string, string) error
	Edit(context.Context, EditRequest) (EditResult, error)
}

type EditRequest struct {
	Path      string
	OldString string
	NewString string
}

type EditResult struct {
	Path    string
	Applied bool
}
