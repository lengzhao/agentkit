package memory

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/lengzhao/agentkit/runtime/configfile"
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
type MemoryPolicyStore struct {
	Path string
}

func (s *MemoryPolicyStore) Load() (MemoryWritePolicy, error) {
	if s.Path == "" {
		return MemoryWritePolicy{}, fmt.Errorf("policy path is required")
	}
	data, err := os.ReadFile(s.Path)
	if err != nil {
		if os.IsNotExist(err) {
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

func (s *MemoryPolicyStore) Save(p MemoryWritePolicy) error {
	if s.Path == "" {
		return fmt.Errorf("policy path is required")
	}
	p = p.Normalized()
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	return configfile.WriteAtomic(s.Path, []byte(raw), 0o644)
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
