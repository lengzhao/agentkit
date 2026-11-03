package feishu

import (
	"sync"
	"time"

	"github.com/lengzhao/agentkit"
)

// streamState holds one reply card and its per-turn presentation state.
type streamState struct {
	mu                 sync.Mutex
	progressHandle     any
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
	bodyFlushTimer     *time.Timer
	keepaliveTimer     *time.Timer
	// 卡片创建失败退避：连续失败次数与下次允许重试的时间，避免网关持续故障时
	// 每个流式 delta 都打一次创建 API。
	createFailCount int
	nextCreateAfter time.Time
	// An uncertain create must not be retried by a later streaming flush.
	unknownCreateErr error
	createDone       chan struct{}
	ended            bool
	// updateFailCount 是同一 handle 上连续更新失败的次数（成功 flush 清零），
	// 仅作日志观测：更新模式下失败不触发任何重建，下轮 flush 自然重试同一张卡。
	updateFailCount int
	// bodyStreamClosed 为 true 表示 CardKit 实体流式模式已关闭（300309），
	// 本卡后续 flush 直接走整卡 patch；重建新卡时重置。
	bodyStreamClosed bool
	// bodyFn 非 nil 时替代 richCardDisplayBody 渲染卡片正文（调用方须持有 st.mu）。
	// 用于后台委派进度卡等自定义正文场景，其余卡片流程（flush/心跳/重建）完全共用。
	bodyFn                      func(streaming bool) string
	richCardPanelVersion        uint64
	richCardFlushedPanelVersion uint64
	lastRichCardBodyStreamRunes int
	// route 是该 stream 的 IM 投递地址（从首个出站事件的 Route 捕获），
	// 供心跳、防抖 flush、turn/end 定稿等异步路径使用，无需再查事件。
	route    agentkit.RouteRef
	hasRoute bool
}

func (st *streamState) lock()   { st.mu.Lock() }
func (st *streamState) unlock() { st.mu.Unlock() }
