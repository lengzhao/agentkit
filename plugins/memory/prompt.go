package memory

import (
	"context"
	"errors"
	"os"
	"strings"

	"github.com/lengzhao/agentkit/cap/workspace"
	rtmem "github.com/lengzhao/agentkit/runtime/memory"
)

func (s *Service) PromptBody(ctx context.Context) (string, error) {
	var merged []rtmem.MemoryEntry
	for _, rel := range s.promptMemoryRelPaths() {
		data, err := s.fs.Read(ctx, rel)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return "", err
		}
		for _, e := range rtmem.ParseMemory(string(data)) {
			if strings.TrimSpace(e.Content) == "" {
				continue
			}
			merged, _ = rtmem.MergeMemoryAdd(merged, e.Content)
		}
	}
	return rtmem.FormatMemoryPromptBody(merged), nil
}

// promptMemoryRelPaths returns workspace-relative files to merge for injection.
// Primary file follows memoryRoot + memoryFile (same as memoryStore). When primary
// is tenant-local, global memory.md is merged first (multi-tenant default).
func (s *Service) promptMemoryRelPaths() []string {
	file := strings.TrimSpace(s.memoryFile)
	if file == "" {
		file = rtmem.DefaultFile
	}
	primary := rtmem.MemoryFileRel(s.memoryRoot, file)
	var rels []string
	if !strings.HasPrefix(primary, workspace.ScopeGlobal+":") {
		globalRel := workspace.ScopeGlobal + ":" + file
		if globalRel != primary {
			rels = append(rels, globalRel)
		}
	}
	rels = append(rels, primary)
	return dedupeRelPaths(rels)
}

func dedupeRelPaths(rels []string) []string {
	seen := make(map[string]struct{}, len(rels))
	var out []string
	for _, rel := range rels {
		rel = strings.TrimSpace(rel)
		if rel == "" {
			continue
		}
		if _, ok := seen[rel]; ok {
			continue
		}
		seen[rel] = struct{}{}
		out = append(out, rel)
	}
	return out
}
