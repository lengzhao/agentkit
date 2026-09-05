// Package filesystem holds shared request/result types used by file tools.
// It is not a swappable capability: tools implement their own ops surface and
// only import these DTOs for grep/find/list formatting. Gitignore helpers live
// in runtime/filesystem.
package filesystem

type DirEntry struct {
	Name  string
	Path  string
	IsDir bool
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
