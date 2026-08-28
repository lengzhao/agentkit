package schedule

import (
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/lengzhao/agentkit"
)

const (
	sessionFresh = "fresh"
	sessionFixed = "fixed"
)

func resolveSessionMode(mode string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "", sessionFresh:
		return sessionFresh, nil
	case sessionFixed:
		return sessionFixed, nil
	default:
		return "", fmt.Errorf("unknown sessionMode %q: use fresh or fixed", mode)
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
	if n.mode == sessionFixed {
		return agentkit.SessionID(n.base + ":default")
	}
	return agentkit.SessionID(fmt.Sprintf("%s:%s:run-%d", n.base, n.stamp, run+1))
}
