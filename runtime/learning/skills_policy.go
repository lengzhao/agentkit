package learning

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/lengzhao/agentkit/runtime/configfile"
)

// SkillsPolicy is tenant-local skill workshop capture mode.
type SkillsPolicy struct {
	SkillsMode string `json:"skillsMode,omitempty"` // off | propose | auto
}

func (p SkillsPolicy) Normalized() SkillsPolicy {
	p.SkillsMode = strings.ToLower(strings.TrimSpace(p.SkillsMode))
	return p
}

// SkillsPolicyStore persists learning/policy.json (relative to memory root).
type SkillsPolicyStore struct {
	Path string
}

func (s *SkillsPolicyStore) Load() (SkillsPolicy, error) {
	if s.Path == "" {
		return SkillsPolicy{}, fmt.Errorf("policy path is required")
	}
	data, err := os.ReadFile(s.Path)
	if err != nil {
		if os.IsNotExist(err) {
			return SkillsPolicy{}, nil
		}
		return SkillsPolicy{}, err
	}
	var p SkillsPolicy
	if err := json.Unmarshal(data, &p); err != nil {
		return SkillsPolicy{}, err
	}
	return p.Normalized(), nil
}

func (s *SkillsPolicyStore) Save(p SkillsPolicy) error {
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

// FormatSkillsPolicyStatus renders /learn policy output.
func FormatSkillsPolicyStatus(skillsMode string, fromFile bool) string {
	src := "config"
	if fromFile {
		src = "learning/policy.json"
	}
	return fmt.Sprintf("skills policy (source: %s):\n  workshop capture: %s\n\noff | propose (pending apply) | auto (apply when safe)",
		src, skillsMode)
}
