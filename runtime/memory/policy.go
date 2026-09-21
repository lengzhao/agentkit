package memory

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/lengzhao/agentkit/cap/filesystem"
)

// MemoryWritePolicy is tenant-local background-review memory write mode.
type MemoryWritePolicy struct {
	MemoryWrite string `json:"memoryWrite,omitempty"` // approve | auto
}

func (p MemoryWritePolicy) Normalized() MemoryWritePolicy {
	p.MemoryWrite = strings.ToLower(strings.TrimSpace(p.MemoryWrite))
	return p
}

// MemoryPolicyStore persists memory/policy.json under memoryRoot.
// Path is a filesystem.Service-relative path.
type MemoryPolicyStore struct {
	FS   filesystem.Service
	Path string
}

func (s *MemoryPolicyStore) Load(ctx context.Context) (MemoryWritePolicy, error) {
	if s.Path == "" {
		return MemoryWritePolicy{}, fmt.Errorf("policy path is required")
	}
	data, err := s.FS.Read(ctx, s.Path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return MemoryWritePolicy{}, nil
		}
		return MemoryWritePolicy{}, err
	}
	var p MemoryWritePolicy
	if err := json.Unmarshal(data, &p); err != nil {
		return MemoryWritePolicy{}, err
	}
	return p.Normalized(), nil
}

func (s *MemoryPolicyStore) Save(ctx context.Context, p MemoryWritePolicy) error {
	if s.Path == "" {
		return fmt.Errorf("policy path is required")
	}
	p = p.Normalized()
	raw, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	return s.FS.Write(ctx, s.Path, raw)
}

// FormatMemoryPolicyStatus renders /memory policy output.
func FormatMemoryPolicyStatus(memoryWrite string, fromFile bool) string {
	src := "config"
	if fromFile {
		src = "memory/policy.json"
	}
	return fmt.Sprintf("memory write policy (source: %s):\n  background-review: %s\n\napprove = staged until /memory approve; auto = write memory.md directly",
		src, memoryWrite)
}
