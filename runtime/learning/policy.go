package learning

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/lengzhao/agentkit/runtime/configfile"
)

// Policy is tenant-local overrides for memory/skill write behavior (/learn policy).
type Policy struct {
	// MemoryWrite: approve (stage background-review) or auto (write memory.md directly).
	MemoryWrite string `json:"memoryWrite,omitempty"`
	// SkillsMode: off, propose (pending workshop), or auto (apply when scanner passes).
	SkillsMode string `json:"skillsMode,omitempty"`
}

// PolicyStore persists policy.json under the tenant workspace.
type PolicyStore struct {
	Path string
}

func (s *PolicyStore) Load() (Policy, error) {
	if s.Path == "" {
		return Policy{}, fmt.Errorf("policy path is required")
	}
	data, err := os.ReadFile(s.Path)
	if err != nil {
		if os.IsNotExist(err) {
			return Policy{}, nil
		}
		return Policy{}, err
	}
	var p Policy
	if err := json.Unmarshal(data, &p); err != nil {
		return Policy{}, err
	}
	return p.Normalized(), nil
}

func (s *PolicyStore) Save(p Policy) error {
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
	return configfile.WriteAtomic(s.Path, raw, 0o644)
}

func (s *PolicyStore) Clear() error {
	if s.Path == "" {
		return fmt.Errorf("policy path is required")
	}
	if err := os.Remove(s.Path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func (p Policy) Normalized() Policy {
	p.MemoryWrite = strings.ToLower(strings.TrimSpace(p.MemoryWrite))
	p.SkillsMode = strings.ToLower(strings.TrimSpace(p.SkillsMode))
	return p
}

// FormatPolicyStatus renders effective policy for /learn policy.
func FormatPolicyStatus(memoryWrite, skillsMode string, fromFile bool) string {
	src := "config"
	if fromFile {
		src = "policy.json"
	}
	return fmt.Sprintf("write policy (source: %s):\n  memory (background-review): %s\n  skills: %s\n\nmemory: approve = staged until /learn approve; auto = write memory.md directly\nskills: off | propose (workshop pending) | auto (apply when safe)",
		src, memoryWrite, skillsMode)
}
