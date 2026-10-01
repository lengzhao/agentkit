package feishu

import (
	"sync"
	"time"

	"github.com/lengzhao/agentkit"
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
	// 增量重建：故障重建新卡时旧卡不删除，新卡只展示旧卡未成功展示的增量，
	// 避免同一内容在多张卡重复刷屏。cardBodyBase/cardStepsBase 是当前活跃卡
	// 创建时全局已展示的正文前缀/步骤数；ackedBody/ackedSteps 是全局已成功
	// 展示（flush 成功）到的位置，重建时作为新卡的 base。
	cardBodyBase string
	cardStepsBase int
	ackedBody    string
	ackedSteps   int
	// route 是该 stream 的 IM 投递地址（从首个出站事件的 Route 捕获），
	// 供心跳、防抖 flush、turn/end 定稿等异步路径使用，无需再查事件。
	route     agentkit.RouteRef
	hasRoute  bool
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
