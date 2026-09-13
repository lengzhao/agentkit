package memory

import "context"

// MemoryEntry is one §-delimited block in memory.md.
type MemoryEntry struct {
	Content string
	Meta    string
}

// MemoryToolInput is the main-agent memory tool schema.
type MemoryToolInput struct {
	Action  string `json:"action" jsonschema:"add, replace, or remove"`
	Content string `json:"content,omitempty" jsonschema:"Entry text for add or replace. Required for add and replace."`
	OldText string `json:"old_text,omitempty" jsonschema:"Required for replace and remove: a short unique substring of one existing entry. Omit for add."`
}

// MemoryToolOutput is returned to the model as JSON.
type MemoryToolOutput struct {
	Success        bool     `json:"success"`
	Message        string   `json:"message,omitempty"`
	Error          string   `json:"error,omitempty"`
	CurrentEntries []string `json:"current_entries,omitempty"`
	Usage          string   `json:"usage,omitempty"`
}

// AddOutcome describes how a write changed memory.md.
type AddOutcome string

const (
	AddOutcomeAdded     AddOutcome = "added"
	AddOutcomeDuplicate AddOutcome = "duplicate"
	AddOutcomeReplaced  AddOutcome = "replaced"
	AddOutcomeRemoved   AddOutcome = "removed"
)

// Tool is the main-agent personal memory tool.
type Tool interface {
	MemoryTool(ctx context.Context, in MemoryToolInput) (MemoryToolOutput, error)
}

// Reader loads memory for prompt injection and display.
type Reader interface {
	LoadEntries(ctx context.Context) ([]MemoryEntry, int, int, error)
	PromptBody(ctx context.Context) (string, error)
}

// Capture applies learn_capture memory actions during background review.
type Capture interface {
	CaptureMemoryAdd(ctx context.Context, text, source string) (string, error)
	CaptureMemoryReplace(ctx context.Context, oldText, content, source string) (string, error)
	CaptureMemoryRemove(ctx context.Context, oldText string) (string, error)
}

// Staging lists and resolves staged memory awaiting approval.
type Staging interface {
	ListStaged(ctx context.Context) ([]StagedEntry, error)
	ApproveStaged(ctx context.Context, id string) (string, error)
	RejectStaged(ctx context.Context, id string) (string, error)
}

// StagedEntry is one pending memory write.
type StagedEntry struct {
	ID      string
	Source  string
	Content string
}

// CommitObserver is notified after memory.md commits (e.g. learning records dreaming signals).
type CommitObserver interface {
	OnMemoryCommitted(ctx context.Context, text, source string, outcome AddOutcome)
}

// CommitObserverRegistrar is implemented by memory/default for post-build wiring without config cycles.
type CommitObserverRegistrar interface {
	RegisterCommitObserver(CommitObserver)
}

// Service is the default memory/default plugin surface.
type Service interface {
	Tool
	Reader
	Capture
	Staging
	Disabled() bool
	// BackgroundReviewRequiresStaging is true when review memory_add should stage instead of writing memory.md.
	BackgroundReviewRequiresStaging(ctx context.Context) bool
	// ResolveRel resolves a path under configured memoryRoot (for dreaming/review sidecars).
	ResolveRel(ctx context.Context, parts ...string) (string, error)
}
