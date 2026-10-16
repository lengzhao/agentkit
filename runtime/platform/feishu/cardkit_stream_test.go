package feishu

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	lark "github.com/larksuite/oapi-sdk-go/v3"
)

// newCardkitMockPlatform 返回一个指向 httptest mock server 的 Platform。
// handler 需自行处理 tenant token 与 cardkit 端点，见 cardkitMockHandler。
func newCardkitMockPlatform(handler http.HandlerFunc) (*Platform, *httptest.Server) {
	srv := httptest.NewServer(handler)
	p := &Platform{
		platformTag: "lark",
		appID:       "cli_test",
		appSecret:   "secret",
		client:      lark.NewClient("cli_test", "secret", lark.WithOpenBaseUrl(srv.URL)),
	}
	return p, srv
}

// cardkitMockHandler 包装 handler，自动应答 tenant_access_token 获取请求。
func cardkitMockHandler(cardFn http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/open-apis/auth/v3/tenant_access_token") {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"code":0,"msg":"ok","tenant_access_token":"tat-mock","expire":7200}`))
			return
		}
		cardFn(w, r)
	}
}

func TestBuildRichCardProgressPanelCollapsed(t *testing.T) {
	card := buildRichCard(cardStatusWorking, "", []toolStep{
		{Kind: toolStepKindTool, Name: "Read", Summary: "README.md"},
	}, "", true, 5*time.Second)
	if !strings.Contains(card, `"expanded":false`) {
		t.Fatalf("expected collapsed progress panel, got %q", card)
	}
	if !strings.Contains(card, `"streaming_mode":true`) {
		t.Fatalf("expected streaming mode during progress updates, got %q", card)
	}
	if !strings.Contains(card, "处理中 · 1 个工具") || !strings.Contains(card, "⏱") {
		t.Fatalf("expected panel title with tools and elapsed time, got %q", card)
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(card), &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, ok := parsed["header"]; ok {
		t.Fatalf("in-progress rich card should omit top header, got %q", card)
	}
}

func TestBuildRichCardDoneShowsStatusLine(t *testing.T) {
	card := buildRichCard(cardStatusDone, "", nil, "hello", false, 3*time.Second)
	if !strings.Contains(card, "☑️ 用时") || !strings.Contains(card, "hello") {
		t.Fatalf("expected done status line and body, got %q", card)
	}
	if strings.Contains(card, `"header"`) {
		t.Fatalf("simplified card should not use top header, got %q", card)
	}
}

func TestRichCardBodyMarkdownDone(t *testing.T) {
	got := richCardBodyMarkdown(cardStatusDone, "reply", 2*time.Second)
	if !strings.HasPrefix(got, "☑️ 用时") || !strings.Contains(got, "reply") {
		t.Fatalf("unexpected body markdown: %q", got)
	}
}

func TestBuildIMCardEntityContent(t *testing.T) {
	content := buildIMCardEntityContent("7355372766134157313")
	if !strings.Contains(content, `"type":"card"`) {
		t.Fatalf("unexpected content: %s", content)
	}
	if !strings.Contains(content, `"card_id":"7355372766134157313"`) {
		t.Fatalf("unexpected content: %s", content)
	}
}

func TestUpdateCardEntitySeqConflictIsBenign(t *testing.T) {
	var calls atomic.Int32
	p, srv := newCardkitMockPlatform(cardkitMockHandler(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":300317,"msg":"ErrMsg: sequence number compare failed; "}`))
	}))
	defer srv.Close()

	h := &feishuPreviewHandle{messageID: "m1", cardID: "c1"}
	if err := p.updateCardEntity(context.Background(), h, `{"schema":"2.0"}`); err != nil {
		t.Fatalf("300317 must be treated as benign (stale frame dropped), got %v", err)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("300317 must not be retried, got %d calls", got)
	}
}

func TestStreamCardElementSeqConflictIsBenign(t *testing.T) {
	var calls atomic.Int32
	p, srv := newCardkitMockPlatform(cardkitMockHandler(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":300317,"msg":"ErrMsg: sequence number compare failed; "}`))
	}))
	defer srv.Close()

	h := &feishuPreviewHandle{messageID: "m1", cardID: "c1"}
	if err := p.streamCardElementByID(context.Background(), h, "main_text", "hello"); err != nil {
		t.Fatalf("300317 on element stream must be benign, got %v", err)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("300317 must not be retried, got %d calls", got)
	}
}

func TestUpdateCardEntityRealErrorPropagates(t *testing.T) {
	p, srv := newCardkitMockPlatform(cardkitMockHandler(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":300309,"msg":"ErrMsg: streaming mode is closed; "}`))
	}))
	defer srv.Close()

	h := &feishuPreviewHandle{messageID: "m1", cardID: "c1"}
	err := p.updateCardEntity(context.Background(), h, `{"schema":"2.0"}`)
	if err == nil || !strings.Contains(err.Error(), "300309") {
		t.Fatalf("non-300317 errors must propagate, got %v", err)
	}
}

// TestCardKitWritesSerializedPerHandle 是 300317 根因的回归测试：同一 card_id
// 的并发更新必须串行到达（取 seq + HTTP 往返整体持 reqMu），否则请求乱序落地
// 会被服务端以 sequence number compare failed 拒绝。
func TestCardKitWritesSerializedPerHandle(t *testing.T) {
	var inFlight atomic.Int32
	var maxInFlight atomic.Int32
	var calls atomic.Int32
	p, srv := newCardkitMockPlatform(cardkitMockHandler(func(w http.ResponseWriter, r *http.Request) {
		cur := inFlight.Add(1)
		for {
			old := maxInFlight.Load()
			if cur <= old || maxInFlight.CompareAndSwap(old, cur) {
				break
			}
		}
		time.Sleep(10 * time.Millisecond) // 放大乱序窗口：无串行时并发必然 >1
		inFlight.Add(-1)
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"msg":"success"}`))
	}))
	defer srv.Close()

	h := &feishuPreviewHandle{messageID: "m1", cardID: "c1"}
	const workers = 8
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- p.updateCardEntity(context.Background(), h, `{"schema":"2.0"}`)
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("updateCardEntity: %v", err)
		}
	}
	if got := calls.Load(); got != workers {
		t.Fatalf("expected %d card update calls, got %d", workers, got)
	}
	if got := maxInFlight.Load(); got != 1 {
		t.Fatalf("card writes must be serialized per handle, max in-flight = %d", got)
	}
	// 串行 + 无失败：seq 应连续递增且未被跳过。
	h.mu.Lock()
	seq := h.sequence
	h.mu.Unlock()
	if seq != workers {
		t.Fatalf("expected sequence %d after %d serialized writes, got %d", workers, workers, seq)
	}
}
