package memory

import (
	"context"
	"fmt"
	"strings"

	capmemory "github.com/lengzhao/agentkit/cap/memory"
	"github.com/lengzhao/agentkit/cap/workspace"
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
	Workspace workspace.Service `json:"workspace"`
}

// Service owns tenant memory.md, ledger, staged review, and /memory commands.
type Service struct {
	disabled   bool
	charLimit  int
	memoryRoot string
	memoryFile string
	review     ReviewConfig
	workspace  workspace.Service
	observers  []capmemory.CommitObserver
}

// New registers memory/default.
func New(cfg Config, deps Deps) (*Service, error) {
	if deps.Workspace == nil {
		return nil, fmt.Errorf("memory/default requires workspace")
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
		workspace:  deps.Workspace,
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

func (s *Service) ResolveRel(ctx context.Context, parts ...string) (string, error) {
	rel := rtmem.JoinUnderRoot(s.memoryRoot, parts...)
	return s.workspace.Resolve(ctx, rel)
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
	store := rtmem.NewMemoryStore(path, limit)
	store.DocName = "memory.md"
	return store, nil
}

func (s *Service) memoryLedger(ctx context.Context) (*rtmem.MemoryLedger, error) {
	path, err := s.workspace.Resolve(ctx, rtmem.LedgerRel(s.memoryRoot))
	if err != nil {
		return nil, err
	}
	return &rtmem.MemoryLedger{Path: path}, nil
}

func (s *Service) stagedStore(ctx context.Context) (*rtmem.StagedStore, error) {
	dir, err := s.workspace.Resolve(ctx, rtmem.StagedDirRel(s.memoryRoot))
	if err != nil {
		return nil, err
	}
	return &rtmem.StagedStore{Dir: dir}, nil
}

func (s *Service) LoadEntries(ctx context.Context) ([]capmemory.MemoryEntry, int, int, error) {
	store, err := s.memoryStore(ctx)
	if err != nil {
		return nil, 0, 0, err
	}
	entries, err := store.Load()
	if err != nil {
		return nil, 0, 0, err
	}
	out := make([]capmemory.MemoryEntry, len(entries))
	for i, e := range entries {
		out[i] = capmemory.MemoryEntry{Content: e.Content, Meta: e.Meta}
	}
	return out, store.TotalChars(entries), store.CharLimit, nil
}
