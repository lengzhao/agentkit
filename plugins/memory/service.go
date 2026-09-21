package memory

import (
	"context"
	"fmt"
	"strings"

	capmemory "github.com/lengzhao/agentkit/cap/memory"
	"github.com/lengzhao/agentkit/cap/filesystem"
	rtmem "github.com/lengzhao/agentkit/runtime/memory"
)

// ReviewConfig controls background-review memory writes.
type ReviewConfig struct {
	WriteApproval *bool `json:"writeApproval"`
}

type Config struct {
	Disabled   bool         `json:"disabled"`
	CharLimit  int          `json:"charLimit"`
	MemoryRoot string       `json:"memoryRoot"`
	MemoryFile string       `json:"memoryFile"`
	Review     ReviewConfig `json:"review"`
}

type Deps struct {
	FS filesystem.Service `json:"fs"`
}

// Service owns tenant memory.md, ledger, staged review, and /memory commands.
type Service struct {
	disabled   bool
	charLimit  int
	memoryRoot string
	memoryFile string
	review     ReviewConfig
	fs         filesystem.Service
	observers  []capmemory.CommitObserver
}

// New registers memory/default.
func New(cfg Config, deps Deps) (*Service, error) {
	if deps.FS == nil {
		return nil, fmt.Errorf("memory/default requires fs dependency")
	}
	root := strings.TrimSpace(cfg.MemoryRoot)
	if root == "" {
		root = rtmem.DefaultRoot
	}
	file := strings.TrimSpace(cfg.MemoryFile)
	if file == "" {
		file = rtmem.DefaultFile
	}
	return &Service{
		disabled:   cfg.Disabled,
		charLimit:  cfg.CharLimit,
		memoryRoot: root,
		memoryFile: file,
		review:     cfg.Review,
		fs:         deps.FS,
	}, nil
}

func (s *Service) Disabled() bool { return s.disabled }

// RegisterCommitObserver appends a post-commit hook (learning/default registers in New).
func (s *Service) RegisterCommitObserver(o capmemory.CommitObserver) {
	if o == nil {
		return
	}
	s.observers = append(s.observers, o)
}

// ResolveRel returns the filesystem-relative path under memoryRoot (may carry a
// global:/local: scope prefix). Callers read/write it through the injected fs.
func (s *Service) ResolveRel(_ context.Context, parts ...string) (string, error) {
	return rtmem.JoinUnderRoot(s.memoryRoot, parts...), nil
}

func (s *Service) memoryStore(ctx context.Context) (*rtmem.MemoryStore, error) {
	path, err := s.ResolveRel(ctx, s.memoryFile)
	if err != nil {
		return nil, err
	}
	limit := s.charLimit
	if limit <= 0 {
		limit = rtmem.DefaultMemoryCharLimit
	}
	store := rtmem.NewMemoryStore(s.fs, path, limit)
	store.DocName = "memory.md"
	return store, nil
}

func (s *Service) memoryLedger(_ context.Context) (*rtmem.MemoryLedger, error) {
	return &rtmem.MemoryLedger{FS: s.fs, Path: rtmem.LedgerRel(s.memoryRoot)}, nil
}

func (s *Service) stagedStore(_ context.Context) (*rtmem.StagedStore, error) {
	return &rtmem.StagedStore{FS: s.fs, Dir: rtmem.StagedDirRel(s.memoryRoot)}, nil
}

func (s *Service) LoadEntries(ctx context.Context) ([]capmemory.MemoryEntry, int, int, error) {
	store, err := s.memoryStore(ctx)
	if err != nil {
		return nil, 0, 0, err
	}
	entries, err := store.Load(ctx)
	if err != nil {
		return nil, 0, 0, err
	}
	out := make([]capmemory.MemoryEntry, len(entries))
	for i, e := range entries {
		out[i] = capmemory.MemoryEntry{Content: e.Content, Meta: e.Meta}
	}
	return out, store.TotalChars(entries), store.CharLimit, nil
}
