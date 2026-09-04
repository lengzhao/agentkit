// Package subagent defines the delegation capability: running a scoped child
// agent and bringing back only its conclusion.
//
// The value is context isolation. A child that burns twenty grep results to
// answer one question keeps those twenty results in its own session; the parent
// sees one Result.Summary. That is why Request carries a self-contained Task
// rather than a slice of the parent's history.
//
// Like the other cap packages this one stays free of the root agentkit import so
// that consumers (tool plugins) and providers (runtime/subagent) can be swapped
// independently. Session and agent identifiers are therefore plain strings here.
package subagent

import "context"

// Definition is one delegatable agent. Providers load these from disk — see
// runtime/subagent for the agents/<name>.md format — so adding a child agent is
// adding a file, not editing the instance graph.
const (
	BackendInprocess = "inprocess"
	BackendLoop      = "loop"
)

type Definition struct {
	Name string `json:"name"`
	// Description is how the parent picks who to delegate to, so it is required.
	Description string `json:"description"`
	// Prompt is the child's persona, layered on top of the shared prompt sections.
	Prompt string `json:"prompt"`
	// Backend selects the runtime: inprocess (default) or loop (a configured Loop agent).
	Backend string `json:"backend,omitempty"`
	// LoopAgent is the Loop agent id when Backend is loop. Defaults to Name.
	LoopAgent string `json:"loopAgent,omitempty"`
	// Async is the default delegation mode for this definition.
	Async bool `json:"async,omitempty"`
	// Tools narrows the child's tool set by name. Empty means every tool the
	// provider was given.
	Tools []string `json:"tools,omitempty"`
	// Skills narrows the skill catalog and skill tool by name. Empty means every
	// skill the provider was given.
	Skills   []string `json:"skills,omitempty"`
	Model    string   `json:"model,omitempty"`
	MaxSteps int      `json:"maxSteps,omitempty"`
	// Path is the file this definition came from, for error messages.
	Path string `json:"path,omitempty"`
}

// Request is one delegation. Task must stand on its own: the child starts from
// an empty session and cannot see the parent's conversation.
type Request struct {
	Agent string `json:"agent"`
	Task  string `json:"task"`
	// Async overrides the definition default. Nil means use the definition.
	Async *bool `json:"async,omitempty"`
}

// Result statuses. Completed and Blocked mirror the child's explicit finish;
// Stopped means it ran out of steps or simply stopped calling tools.
const (
	StatusCompleted = "completed"
	StatusBlocked   = "blocked"
	StatusStopped   = "stopped"
	StatusRunning   = "running"
)

// Result is everything that crosses back into the parent's context. Summary is
// the answer; Session is kept for audit so a reviewer can open the child's log.
type Result struct {
	Agent   string `json:"agent"`
	Session string `json:"session"`
	Status  string `json:"status"`
	Summary string `json:"summary"`
	Steps   int    `json:"steps"`
	// JobID is set for async delegations that return before the child finishes.
	JobID string `json:"jobId,omitempty"`
}

// Spawner runs child agents.
//
// Run is usually synchronous: inprocess children block until they finish. Loop-backed
// children may return status=running when async is requested; the conclusion is
// delivered later via a follow-up turn. Parallel fan-out per parent session is
// still limited to one running async job today.
type Spawner interface {
	// Definitions lists who can be delegated to, re-read per call so editing a
	// definition file takes effect without a restart.
	Definitions(context.Context) ([]Definition, error)
	Run(context.Context, Request) (Result, error)
}
