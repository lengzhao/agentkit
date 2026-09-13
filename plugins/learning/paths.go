package learning

import (
	"fmt"
	"strings"

	"github.com/lengzhao/agentkit/plugins/learning/dreaming"
)

const (
	DefaultDreamsFile     = "DREAMS.md"
	DefaultDreamingSubdir = "memory/dreaming"
)

// FormatHelp returns /learn usage text.
func FormatHelp() string {
	return `Usage:
  /learn                         show this help
  /learn session                 queue session user messages as dreaming signals (not memory.md)
  /learn dream status            show dreaming state
  /learn dream run               run Light → REM → Deep sweep now
  /learn dream on|off            enable or disable background dreaming
  /learn skill [focus]           create a skill workshop proposal from session
  /learn workshop list           list pending skill proposals
  /learn workshop show <id>      show one proposal
  /learn workshop apply <id>     apply a pending proposal to skills/
  /learn workshop reject <id>    reject a pending proposal
  /learn policy                  show skills write policy (memory: /memory policy)
  /learn policy skills off|propose|auto
  /learn policy reset            use config defaults again
  /learn help                    show this help

Notes:
  - Personal memory: /memory show|add|pending|approve|policy
  - Dream Diary lives in DREAMS.md (human review only, not injected)
  - Skill proposals live under skills/.workshop/ until applied
  - Background review is the main automatic path to memory.md`
}

func formatSweepResult(res *dreaming.SweepResult) string {
	if res == nil {
		return "dreaming sweep completed"
	}
	return fmt.Sprintf("dreaming sweep: sessions=%d signals=%d review-eligible=%d skipped=%d themes=%s",
		res.SessionsIngested, res.SignalsIngested, res.Eligible, res.Skipped, strings.Join(res.Themes, ", "))
}
