package schedule

import (
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/lengzhao/agentkit"
	capschedule "github.com/lengzhao/agentkit/cap/schedule"
)

func resolveSessionMode(mode string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "", capschedule.SessionModeStateless, capschedule.SessionModeFresh:
		return capschedule.SessionModeStateless, nil
	case capschedule.SessionModeReuse:
		return capschedule.SessionModeReuse, nil
	case capschedule.SessionModeFixed:
		return capschedule.SessionModeFixed, nil
	default:
		return "", fmt.Errorf("unknown sessionMode %q: use stateless, reuse, or fixed", mode)
	}
}

var namerSeq atomic.Int64

type sessionNamer struct {
	mode  string
	base  string
	stamp string
}

func newSessionNamer(mode, base string, now func() time.Time) sessionNamer {
	if now == nil {
		now = time.Now
	}
	stamp := fmt.Sprintf("%s-p%d-%d",
		now().UTC().Format("20060102-150405.000"),
		os.Getpid(),
		namerSeq.Add(1),
	)
	return sessionNamer{mode: mode, base: base, stamp: stamp}
}

func (n sessionNamer) forRun(run int) agentkit.SessionID {
	if n.mode == capschedule.SessionModeFixed {
		return agentkit.SessionID(n.base + ":default")
	}
	return agentkit.SessionID(fmt.Sprintf("%s:%s:run-%d", n.base, n.stamp, run+1))
}

func statelessSessionID(job capschedule.Job, now time.Time) agentkit.SessionID {
	id := strings.TrimSpace(job.ID)
	if id == "" {
		id = "job"
	}
	return agentkit.SessionID(fmt.Sprintf("schedule:%s:%d", id, now.UnixNano()))
}
