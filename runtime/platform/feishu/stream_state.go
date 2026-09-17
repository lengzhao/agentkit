package feishu

import (
	"sync"
	"time"
)

// streamStateData is a mutable card snapshot (tests and async subagent panel use this directly).
type streamStateData struct {
	handle                     any
	progressHandle             any
	cards                      []streamCard
	accumulated                string
	bodyText                   string
	committedBodyText          string
	finalizedBodyText          string
	finalizedSteps             []toolStep
	thinking                   string
	steps                      []toolStep
	toolStepIdx                map[int]int
	status                     cardStatus
	startedAt                  time.Time
	progressStartedAt          time.Time
	lastUpdate                 time.Time
	lastProgressUpdate         time.Time
	lastBodyUpdate             time.Time
	bodyFlushTimer             *time.Timer
	legacyFlushTimer           *time.Timer
	richCardPanelVersion       uint64
	richCardFlushedPanelVersion uint64
	lastRichCardBodyStreamRunes int
}

// streamState holds per-stream outbound card state. Access is serialized with
// a mutex (hot path: every streaming delta locks once). lock/unlock are kept as
// methods so call sites read symmetrically; the underlying primitive is a
// sync.Mutex because channel-based locks are measurably slower on this path.
type streamState struct {
	mu sync.Mutex
	streamStateData
}

func newStreamState() *streamState {
	return &streamState{}
}

func (st *streamState) lock()   { st.mu.Lock() }
func (st *streamState) unlock() { st.mu.Unlock() }

// streamStateLiteral builds a stream state for tests (fields pre-populated).
func streamStateLiteral(data streamStateData) *streamState {
	return &streamState{streamStateData: data}
}
