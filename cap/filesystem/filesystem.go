// Package filesystem is the swappable file-store contract used by file tools
// and runtime plugins (memory, schedule, skills, credentials, …).
//
// Paths are slash-separated and interpreted by the backend (workspace-relative
// for local disk, object keys for S3, remote paths for SSH/FUSE, etc.). Empty
// path or "." is the backend root. Implementations should honor the workspace
// scope prefixes "global:" / "local:" (see cap/workspace.ParseScoped) so one
// instance can serve both tenant-local and shared-global files.
//
// Not-found errors must match errors.Is(err, os.ErrNotExist). Writes create
// parent prefixes as needed and must be atomic (temp file + rename on local
// disk; a single PUT on object stores). Implementations may ignore .gitignore
// (local disk respects it; object stores typically do not).
package filesystem

import (
	"context"
	"os"
	"time"
)

// Info is Stat metadata for a file or directory-like prefix.
type Info struct {
	Name    string
	Path    string
	Size    int64
	IsDir   bool
	ModTime time.Time
}

type DirEntry struct {
	Name    string
	Path    string
	IsDir   bool
	ModTime time.Time
}

type GrepRequest struct {
	Pattern    string
	Path       string
	Glob       string
	IgnoreCase bool
	Literal    bool
	Context    int
	MaxMatches int
}

type GrepMatch struct {
	Path    string
	Line    int
	Content string
}

type GrepResult struct {
	Matches   []GrepMatch
	Truncated bool
	Text      string
	Hint      string
}

type FindRequest struct {
	Pattern    string
	Path       string
	MaxResults int
}

type FindResult struct {
	Paths     []string
	Truncated bool
	Text      string
	Hint      string
}

// ReadFS is the read/list surface. Remote and object-store backends implement
// directories as prefixes.
type ReadFS interface {
	Read(ctx context.Context, path string) ([]byte, error)
	Stat(ctx context.Context, path string) (Info, error)
	List(ctx context.Context, path string) ([]DirEntry, error)
}

// WriteOptions customizes a single Write call.
type WriteOptions struct {
	// Perm is the permission for newly created files. Zero keeps an existing
	// file's mode, or 0o644 for new files. Object stores ignore it.
	Perm os.FileMode
}

// WriteOption customizes a single Write call.
type WriteOption func(*WriteOptions)

// WithPerm sets the file permission (e.g. 0o600 for secrets files).
func WithPerm(perm os.FileMode) WriteOption {
	return func(o *WriteOptions) { o.Perm = perm }
}

// WriteFS creates or replaces a file. Parent prefixes are created as needed
// (no-op on stores without directories).
type WriteFS interface {
	Write(ctx context.Context, path string, data []byte, opts ...WriteOption) error
	// Append adds data to the end of path, creating it when missing. Object
	// stores may implement it as read-modify-write.
	Append(ctx context.Context, path string, data []byte) error
}

// SearchFS is content and name search. Local disk walks the tree; S3 may list
// by prefix; a remote host may shell out to rg/find.
type SearchFS interface {
	Grep(ctx context.Context, req GrepRequest) (GrepResult, error)
	Find(ctx context.Context, req FindRequest) (FindResult, error)
}

// Service is the file store injected into tool/fs-workspace and runtime
// plugins. Standard kind: filesystem/local. Alternate backends (memory, S3,
// remote) register as filesystem/<name> and are wired through deps.fs.
type Service interface {
	ReadFS
	WriteFS
	SearchFS
}
