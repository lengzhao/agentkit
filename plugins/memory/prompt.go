package memory

import (
	"context"
	"os"
	"strings"

	"github.com/lengzhao/agentkit/cap/workspace"
	rtmem "github.com/lengzhao/agentkit/runtime/memory"
)

func defaultPromptFilenames() []string {
	return []string{"memory.md", "MEMORY.md"}
}

func (s *Service) PromptBody(ctx context.Context) (string, error) {
	var merged []rtmem.MemoryEntry
	for _, rel := range promptMemoryPaths(defaultPromptFilenames()) {
		path, err := s.workspace.Resolve(ctx, rel)
		if err != nil {
			return "", err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
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

func promptMemoryPaths(filenames []string) []string {
	var out []string
	for _, name := range filenames {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		out = append(out, workspace.ScopeGlobal+":"+name)
	}
	for _, name := range filenames {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		out = append(out, name)
	}
	return out
}
