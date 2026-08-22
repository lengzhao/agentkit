package filesystem

import "context"

type Service interface {
	ReadText(context.Context, string, int) (string, error)
	WriteText(context.Context, string, string) error
	Edit(context.Context, EditRequest) (EditResult, error)
	ListDir(context.Context, string) ([]DirEntry, error)
	Grep(context.Context, GrepRequest) (GrepResult, error)
	Find(context.Context, FindRequest) (FindResult, error)
}

type DirEntry struct {
	Name  string `json:"name"`
	Path  string `json:"path"`
	IsDir bool   `json:"isDir"`
}

type GrepRequest struct {
	Pattern    string
	Path       string
	Glob       string
	IgnoreCase bool
	MaxMatches int
}

type GrepMatch struct {
	Path    string `json:"path"`
	Line    int    `json:"line"`
	Content string `json:"content"`
}

type GrepResult struct {
	Matches   []GrepMatch `json:"matches"`
	Truncated bool        `json:"truncated,omitempty"`
}

type FindRequest struct {
	Pattern    string
	Path       string
	MaxResults int
}

type FindResult struct {
	Paths     []string `json:"paths"`
	Truncated bool     `json:"truncated,omitempty"`
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
