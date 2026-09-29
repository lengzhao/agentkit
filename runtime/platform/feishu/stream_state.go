package feishu

import (
	"sync"
	"time"
)

// streamStateData is a mutable card snapshot (tests and async subagent panel use this directly).
type streamStateData struct {
	progressHandle     any
	cards              []streamCard
	bodyText           string
	committedBodyText  string
	finalizedBodyText  string
	finalizedSteps     []toolStep
	thinking           string
	steps              []toolStep
	toolStepIdx        map[int]int
	status             cardStatus
	startedAt          time.Time
	progressStartedAt  time.Time
	lastProgressUpdate time.Time
	lastBodyUpdate     time.Time
	bodyFlushTimer     *time.Timer
	keepaliveTimer     *time.Timer
	// 卡片创建失败退避：连续失败次数与下次允许重试的时间，避免网关持续故障时
	// 每个流式 delta 都打一次创建 API。
	createFailCount  int
	nextCreateAfter  time.Time
	// bodyStreamClosed 为 true 表示 CardKit 实体流式模式已关闭（300309），
	// 本卡后续 flush 直接走整卡 patch；重建新卡时重置。
	bodyStreamClosed bool
	// bodyFn 非 nil 时替代 richCardDisplayBody 渲染卡片正文（调用方须持有 st.mu）。
	// 用于后台委派进度卡等自定义正文场景，其余卡片流程（flush/心跳/重建）完全共用。
	bodyFn                      func(streaming bool) string
	richCardPanelVersion        uint64
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
