package headless

import (
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/lengzhao/agentkit"
)

// Session id modes shared by worker and timer.
const (
	// SessionFresh gives every task or tick its own session. This is the default
	// because the alternative fails slowly: a periodic job pinned to one session
	// grows its context every tick until it hits the model's window.
	SessionFresh = "fresh"
	// SessionFixed reuses one session id so the agent remembers across runs.
	// Pair it with compaction, or the context grows without bound.
	SessionFixed = "fixed"
)

func resolveSessionMode(mode string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "", SessionFresh:
		return SessionFresh, nil
	case SessionFixed:
		return SessionFixed, nil
	default:
		return "", fmt.Errorf("unknown sessionMode %q: use fresh or fixed", mode)
	}
}

// namerSeq distinguishes namers created within one process, so two timers in the
// same multiplex graph cannot land on the same session.
var namerSeq atomic.Int64

// sessionNamer builds session ids for an unattended run.
type sessionNamer struct {
	mode  string
	base  string
	stamp string
}

func newSessionNamer(mode, base string, now func() time.Time) sessionNamer {
	if now == nil {
		now = time.Now
	}
	// Fresh has to mean fresh, or a "new" session silently reopens an old
	// history. Three parts, each covering what the others miss: the start time
	// sorts runs and reads well, the pid separates concurrent processes that
	// start in the same millisecond, and the counter separates namers inside one
	// process.
	stamp := fmt.Sprintf("%s-p%d-%d",
		now().UTC().Format("20060102-150405.000"),
		os.Getpid(),
		namerSeq.Add(1),
	)
	return sessionNamer{mode: mode, base: base, stamp: stamp}
}

func (n sessionNamer) forRun(run int) agentkit.SessionID {
	if n.mode == SessionFixed {
		return agentkit.SessionID(n.base + ":default")
	}
	return agentkit.SessionID(fmt.Sprintf("%s:%s:run-%d", n.base, n.stamp, run+1))
}
