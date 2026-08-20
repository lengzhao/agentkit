package sandbox

import "context"

type Service interface {
	WrapExec(context.Context, ExecRequest) (ExecPlan, error)
	AuthorizePath(context.Context, PathRequest) (PathDecision, error)
}

type ExecRequest struct{}
type ExecPlan struct{}
type PathRequest struct{}
type PathDecision struct{}
