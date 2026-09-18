package sessstore

import "github.com/lengzhao/agentkit/runtime/session/derive"

// Event payload types now live in the derive subpackage; aliases keep the
// session package API stable while callers migrate.

const (
	TodoPending    = derive.TodoPending
	TodoInProgress = derive.TodoInProgress
	TodoDone       = derive.TodoDone
)

type Todo = derive.Todo

type TodoUpdateData = derive.TodoUpdateData

type RunFinishData = derive.RunFinishData

const (
	FinishCompleted = derive.FinishCompleted
	FinishBlocked   = derive.FinishBlocked
)

type TurnContinueData = derive.TurnContinueData

type UsageData = derive.UsageData

type RunState = derive.RunState

const MetadataLogicalChars = derive.MetadataLogicalChars
