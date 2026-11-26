package taskloom

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/rctx"
)

// fakeTaskloom serves the read endpoints the adapter uses from mutable state.
type fakeTaskloom struct {
	mu      sync.Mutex
	down    bool
	events  []change
	inbox   []pending
	commits map[string][]commitView
	gone    map[string]bool
	auth    string
	floor   int      // cursors below this were cleaned up: 409 resync_required
	reads   []string // task view requests, path and query
}

func newFake() *fakeTaskloom {
	return &fakeTaskloom{commits: map[string][]commitView{}, gone: map[string]bool{}, auth: "Bearer s"}
}

func (f *fakeTaskloom) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.down {
		http.Error(w, "down", http.StatusBadGateway)
		return
	}
	if r.Header.Get("Authorization") != f.auth {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/v1")
	write := func(v any) { _ = json.NewEncoder(w).Encode(v) }
	switch {
	case path == "/me":
		write(viewer{Tenant: "t", Subject: "bot", Kind: "agent"})
	case path == "/changes":
		from := 0
		if c := r.URL.Query().Get("cursor"); c != "" {
			n, err := strconv.Atoi(c)
			if err != nil {
				http.Error(w, "bad cursor", http.StatusBadRequest)
				return
			}
			from = n
			if from < f.floor {
				w.WriteHeader(http.StatusConflict)
				write(map[string]string{"Code": "resync_required"})
				return
			}
		} else {
			from = len(f.events)
		}
		write(changePage{Changes: f.events[from:], Cursor: strconv.Itoa(len(f.events))})
	case path == "/tasks":
		if r.URL.Query().Get("mine") != "true" {
			http.Error(w, "only mine", http.StatusBadRequest)
			return
		}
		rows := []listRow{}
		for _, it := range f.inbox {
			var row listRow
			row.Task.ID, row.Task.Title, row.NextActor, row.ForMe = it.TaskID, it.Title, it.NextActor, it.ForMe
			rows = append(rows, row)
		}
		write(rows)
	case strings.HasPrefix(path, "/tasks/"):
		id := strings.Split(path, "/")[2]
		f.reads = append(f.reads, r.URL.RequestURI())
		if f.gone[id] {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		// Like Taskloom: the newest page (or the one before ?before=) in
		// ascending order; one extra commit tells whether older ones exist.
		before, _ := strconv.ParseInt(r.URL.Query().Get("before"), 10, 64)
		var page []commitView
		for _, c := range f.commits[id] {
			if before == 0 || c.Sequence < before {
				page = append(page, c)
			}
		}
		d := taskDetail{}
		d.Task.ID, d.Task.Title = id, "title "+id
		if len(page) > 100 {
			page, d.Earlier = page[len(page)-100:], true
		}
		d.Commits = page
		write(d)
	default:
		http.NotFound(w, r)
	}
}

var seq int64

func (f *fakeTaskloom) commit(task, author, kind, body string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	seq++
	f.commits[task] = append(f.commits[task], commitView{ID: "c" + strconv.FormatInt(seq, 10), Author: author, Kind: kind, Body: body, Sequence: seq})
	f.events = append(f.events, change{TaskID: task, Kind: "task.changed"})
}

func (f *fakeTaskloom) set(fn func()) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fn()
}

func newTestPlatform(t *testing.T, f *fakeTaskloom) *Platform {
	t.Helper()
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	cfg := Config{BaseURL: srv.URL + "/v1"}
	cfg.SetDefaults()
	p := newPlatform(cfg, newClient(cfg.BaseURL, "Bearer s", time.Second))
	p.sleep = func(ctx context.Context, _ time.Duration) error { return ctx.Err() }
	return p
}

// next returns the next event, or "" when the platform would keep polling.
func next(t *testing.T, p *Platform, polls int) (agentkit.MessageEvent, string) {
	t.Helper()
	for i := 0; i < polls; i++ {
		if len(p.queue) > 0 {
			break
		}
		if err := p.step(context.Background()); err != nil {
			p.fail(err)
		}
	}
	if len(p.queue) == 0 {
		return agentkit.MessageEvent{}, ""
	}
	ev, err := p.Receive(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return ev, ev.Message.Content[0].Text
}

func TestWakesOncePerTaskAndIgnoresOwnCommits(t *testing.T) {
	f := newFake()
	f.commit("A", "alice", "comment", "please do A")
	f.set(func() {
		f.inbox = []pending{{TaskID: "A", Title: "A", NextActor: "assignee", ForMe: true}, {TaskID: "B", NextActor: "assignee"}}
	})
	p := newTestPlatform(t, f)

	_, text := next(t, p, 1)
	if !strings.Contains(text, "任务 A") || strings.Contains(text, "please do A") {
		t.Fatalf("first wake should point at the task without replaying history: %q", text)
	}
	if _, text := next(t, p, 1); text != "" {
		t.Fatalf("blocked task must not wake: %q", text)
	}

	f.commit("A", "bot", "comment", "working on it")
	if _, text := next(t, p, 3); text != "" {
		t.Fatalf("own commit woke the agent: %q", text)
	}

	f.commit("A", "alice", "comment", "also cover B")
	_, text = next(t, p, 3)
	if !strings.Contains(text, "alice") || !strings.Contains(text, "also cover B") || strings.Contains(text, "working on it") {
		t.Fatalf("wake should carry only others' new commits: %q", text)
	}
}

func TestCommitsWhileNotWaitingAreReportedWhenItReturns(t *testing.T) {
	f := newFake()
	f.set(func() { f.inbox = []pending{{TaskID: "A", NextActor: "assignee", ForMe: true}} })
	p := newTestPlatform(t, f)
	next(t, p, 1)

	// The agent asks the owner: the task leaves its inbox until the answer.
	f.set(func() { f.inbox = nil })
	f.commit("A", "bot", "question", "which format?")
	f.commit("A", "alice", "comment", "thinking")
	if _, text := next(t, p, 2); text != "" {
		t.Fatalf("woke while waiting on the owner: %q", text)
	}
	f.set(func() { f.inbox = []pending{{TaskID: "A", NextActor: "assignee", ForMe: true}} })
	f.commit("A", "alice", "answer", "markdown")
	_, text := next(t, p, 2)
	if !strings.Contains(text, "thinking") || !strings.Contains(text, "markdown") || strings.Contains(text, "which format") {
		t.Fatalf("answer wake: %q", text)
	}
}

func TestEachTaskHasItsOwnSession(t *testing.T) {
	f := newFake()
	f.set(func() {
		f.inbox = []pending{{TaskID: "A", NextActor: "assignee", ForMe: true}, {TaskID: "B", NextActor: "addressee", ForMe: true}}
	})
	p := newTestPlatform(t, f)
	a, ta := next(t, p, 1)
	b, _ := next(t, p, 1)
	if !strings.Contains(ta, "任务 A") { // map order decides which wakes first
		a, b = b, a
	}
	ca := rctx.ResolveEnvelope(a, rctx.RoutePolicy{}).Conversation
	cb := rctx.ResolveEnvelope(b, rctx.RoutePolicy{}).Conversation
	if ca == "" || ca == cb {
		t.Fatalf("tasks share a session: %q %q", ca, cb)
	}
}

// A cancelled or lost task leaves the inbox: nothing is sent to its session,
// whether or not a turn is running; Taskloom fences the writes.
func TestLeavingTheInboxSendsNothing(t *testing.T) {
	f := newFake()
	f.set(func() { f.inbox = []pending{{TaskID: "A", NextActor: "assignee", ForMe: true}} })
	p := newTestPlatform(t, f)
	next(t, p, 1)
	f.set(func() { f.inbox, f.gone["A"] = nil, true })
	f.commit("A", "alice", "cancel", "")
	if _, text := next(t, p, 2); text != "" {
		t.Fatalf("session got %q", text)
	}
}

func TestOutageBacksOffAndKeepsCursor(t *testing.T) {
	f := newFake()
	f.set(func() { f.inbox = []pending{{TaskID: "A", NextActor: "assignee", ForMe: true}} })
	p := newTestPlatform(t, f)
	next(t, p, 1)

	f.set(func() { f.down = true })
	f.commit("A", "alice", "comment", "during outage")
	for i := 0; i < 5; i++ {
		if err := p.step(context.Background()); err == nil {
			t.Fatal("expected failure while down")
		} else {
			p.fail(err)
		}
	}
	if p.backoff <= p.poll || p.backoff > p.maxGap {
		t.Fatalf("backoff %v", p.backoff)
	}
	f.set(func() { f.down = false })
	_, text := next(t, p, 1)
	if !strings.Contains(text, "during outage") {
		t.Fatalf("change during outage lost: %q", text)
	}
}

func TestRejectedCursorResynchronizes(t *testing.T) {
	f := newFake()
	f.set(func() { f.inbox = []pending{{TaskID: "A", NextActor: "assignee", ForMe: true}} })
	p := newTestPlatform(t, f)
	next(t, p, 1)
	p.cursor = "not-a-number"
	if err := p.step(context.Background()); err != nil || p.started {
		t.Fatalf("bad cursor: err=%v started=%v", err, p.started)
	}
	if err := p.step(context.Background()); err != nil || !p.started || p.cursor == "not-a-number" {
		t.Fatalf("resync: err=%v started=%v cursor=%q", err, p.started, p.cursor)
	}
}

func TestRejectsHumanCredential(t *testing.T) {
	f := newFake()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(viewer{Subject: "alice", Kind: "human"})
	}))
	defer srv.Close()
	_ = f
	cfg := Config{BaseURL: srv.URL}
	cfg.SetDefaults()
	p := newPlatform(cfg, newClient(srv.URL, "Bearer s", time.Second))
	if err := p.step(context.Background()); err == nil || !strings.Contains(err.Error(), "human") {
		t.Fatalf("human credential accepted: %v", err)
	}
}

func TestReceiveStopsWithContext(t *testing.T) {
	f := newFake()
	cfg := Config{BaseURL: "http://127.0.0.1:1"}
	cfg.SetDefaults()
	p := newPlatform(cfg, newClient(cfg.BaseURL, "Bearer s", 100*time.Millisecond))
	_ = f
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	if _, err := p.Receive(ctx); err == nil {
		t.Fatal("Receive returned without an event or error")
	}
}

// A parent blocked on its required subtasks leaves the inbox; when the last
// one completes it waits on the Agent again with no new commit of its own,
// and that re-entry wakes the Agent to deliver.
func TestReturningToTheInboxWakesWithoutNewCommits(t *testing.T) {
	f := newFake()
	f.set(func() { f.inbox = []pending{{TaskID: "P", NextActor: "assignee", ForMe: true}} })
	p := newTestPlatform(t, f)
	next(t, p, 1)
	f.set(func() { f.inbox = []pending{{TaskID: "P", NextActor: "assignee"}} })
	f.commit("P", "bot", "plan.changed", "split")
	if _, text := next(t, p, 2); text != "" {
		t.Fatalf("woke while blocked: %q", text)
	}
	f.set(func() {
		f.inbox = []pending{{TaskID: "P", NextActor: "assignee", ForMe: true}}
		f.events = append(f.events, change{TaskID: "P", Kind: "task.changed"})
	})
	if _, text := next(t, p, 2); !strings.Contains(text, "任务 P") {
		t.Fatalf("return to the inbox did not wake: %q", text)
	}
	f.set(func() { f.events = append(f.events, change{TaskID: "P", Kind: "task.changed"}) })
	if _, text := next(t, p, 2); text != "" {
		t.Fatalf("woke again while still waiting: %q", text)
	}
}

// A stale cursor (older than the retained events) answers 409; the adapter
// resynchronizes from the task list instead of retrying forever.
func TestExpiredCursorResynchronizes(t *testing.T) {
	f := newFake()
	f.set(func() { f.inbox = []pending{{TaskID: "A", NextActor: "assignee", ForMe: true}} })
	p := newTestPlatform(t, f)
	next(t, p, 1)
	f.commit("A", "alice", "comment", "one")
	f.set(func() { f.floor = len(f.events) + 1 })
	if err := p.step(context.Background()); err != nil || p.started {
		t.Fatalf("expired cursor: err=%v started=%v", err, p.started)
	}
	f.set(func() { f.floor = 0 })
	if err := p.step(context.Background()); err != nil || !p.started {
		t.Fatalf("resync: err=%v started=%v", err, p.started)
	}
}

// A long timeline is read newest page first: the first wake reads one page
// only, and a later wake pages back with ?before= exactly to what it saw.
func TestLongTimelinePagesBackToLastSeen(t *testing.T) {
	f := newFake()
	for i := 0; i < 250; i++ {
		f.commit("A", "alice", "comment", "old "+strconv.Itoa(i))
	}
	f.set(func() { f.inbox = []pending{{TaskID: "A", NextActor: "assignee", ForMe: true}} })
	p := newTestPlatform(t, f)
	f.set(func() { f.reads = nil })
	if _, text := next(t, p, 1); !strings.Contains(text, "任务 A") {
		t.Fatalf("first wake: %q", text)
	}
	if len(f.reads) != 1 || strings.Contains(f.reads[0], "before=") {
		t.Fatalf("first wake should read only the newest page: %v", f.reads)
	}
	for i := 0; i < 150; i++ {
		f.commit("A", "alice", "comment", "new "+strconv.Itoa(i))
	}
	f.set(func() { f.reads = nil })
	_, text := next(t, p, 1)
	if strings.Count(text, "new ") != 150 || strings.Contains(text, "old ") {
		t.Fatalf("catch-up should carry exactly the 150 new commits")
	}
	if strings.Index(text, "new 0") > strings.Index(text, "new 149") {
		t.Fatal("commits out of order")
	}
	if len(f.reads) != 2 || !strings.Contains(f.reads[1], "before=") {
		t.Fatalf("expected one page back: %v", f.reads)
	}
}
