package sessionquery

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/lengzhao/agentkit"
	capsessionindex "github.com/lengzhao/agentkit/cap/sessionindex"
	"github.com/lengzhao/agentkit/testing/agenttest"
)

type stubIndex struct {
	syncErr error
	search  struct {
		query string
		limit int
		hits  []capsessionindex.Hit
		err   error
	}
	list struct {
		limit    int
		sessions []capsessionindex.SessionSummary
		err      error
	}
	scroll struct {
		sessionID       string
		seq             int64
		before, after   int
		msgs            []capsessionindex.MessageRow
		err             error
	}
}

func (s *stubIndex) SyncSessions(context.Context) error { return s.syncErr }

func (s *stubIndex) Search(_ context.Context, query string, limit int) ([]capsessionindex.Hit, error) {
	s.search.query = query
	s.search.limit = limit
	if s.search.err != nil {
		return nil, s.search.err
	}
	return s.search.hits, nil
}

func (s *stubIndex) ListSessions(_ context.Context, limit int) ([]capsessionindex.SessionSummary, error) {
	s.list.limit = limit
	if s.list.err != nil {
		return nil, s.list.err
	}
	return s.list.sessions, nil
}

func (s *stubIndex) ScrollMessages(_ context.Context, sessionID string, anchorSeq int64, before, after int) ([]capsessionindex.MessageRow, error) {
	s.scroll.sessionID = sessionID
	s.scroll.seq = anchorSeq
	s.scroll.before = before
	s.scroll.after = after
	if s.scroll.err != nil {
		return nil, s.scroll.err
	}
	return s.scroll.msgs, nil
}

func callTool(t *testing.T, tool agentkit.Tool, input string) Output {
	t.Helper()
	raw := agenttest.CallTool(t, context.Background(), tool, input)
	var out Output
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatalf("unmarshal %q: %v", raw, err)
	}
	return out
}

func callToolErrText(t *testing.T, tool agentkit.Tool, input string) string {
	t.Helper()
	got, err := tool.Call(context.Background(), json.RawMessage(input))
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	return got
}

func TestNewRequiresIndex(t *testing.T) {
	_, err := New(Config{}, Deps{})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestSessionSearchMode(t *testing.T) {
	idx := &stubIndex{search: struct {
		query string
		limit int
		hits  []capsessionindex.Hit
		err   error
	}{hits: []capsessionindex.Hit{{SessionID: "s1", Seq: 3, Snippet: "hello"}}}}
	tool, err := New(Config{DefaultLimit: 10}, Deps{Index: idx})
	if err != nil {
		t.Fatal(err)
	}
	out := callTool(t, tool, `{"query":"hello"}`)
	if out.Mode != "search" || out.Count != 1 || idx.search.query != "hello" || idx.search.limit != 10 {
		t.Fatalf("search out=%+v idx=%+v", out, idx.search)
	}
}

func TestSessionSearchRequiresQuery(t *testing.T) {
	tool, err := New(Config{}, Deps{Index: &stubIndex{}})
	if err != nil {
		t.Fatal(err)
	}
	msg := callToolErrText(t, tool, `{"mode":"search"}`)
	if msg == "" || !strings.Contains(msg, "query") {
		t.Fatalf("got %q", msg)
	}
}

func TestSessionListMode(t *testing.T) {
	idx := &stubIndex{}
	idx.list.sessions = []capsessionindex.SessionSummary{{SessionID: "a"}, {SessionID: "b"}}
	tool, err := New(Config{}, Deps{Index: idx})
	if err != nil {
		t.Fatal(err)
	}
	out := callTool(t, tool, `{"mode":"list","limit":5}`)
	if out.Mode != "list" || out.Count != 2 || idx.list.limit != 5 {
		t.Fatalf("out=%+v list.limit=%d", out, idx.list.limit)
	}
}

func TestSessionScrollMode(t *testing.T) {
	idx := &stubIndex{}
	idx.scroll.msgs = []capsessionindex.MessageRow{{Seq: 9, Text: "mid"}}
	tool, err := New(Config{}, Deps{Index: idx})
	if err != nil {
		t.Fatal(err)
	}
	out := callTool(t, tool, `{"mode":"scroll","session_id":"s1","seq":0,"before":2,"after":1}`)
	if out.Mode != "scroll" || out.SessionID != "s1" || out.AnchorSeq != 9 {
		t.Fatalf("out=%+v", out)
	}
	if idx.scroll.sessionID != "s1" || idx.scroll.before != 2 || idx.scroll.after != 1 {
		t.Fatalf("scroll args: %+v", idx.scroll)
	}
}

func TestSessionScrollRequiresSessionID(t *testing.T) {
	tool, err := New(Config{}, Deps{Index: &stubIndex{}})
	if err != nil {
		t.Fatal(err)
	}
	msg := callToolErrText(t, tool, `{"mode":"scroll"}`)
	if !strings.Contains(msg, "session_id") {
		t.Fatalf("got %q", msg)
	}
}

func TestSessionLimitCap(t *testing.T) {
	idx := &stubIndex{}
	tool, err := New(Config{DefaultLimit: 10}, Deps{Index: idx})
	if err != nil {
		t.Fatal(err)
	}
	callTool(t, tool, `{"query":"x","limit":100}`)
	if idx.search.limit != 50 {
		t.Fatalf("limit cap: got %d", idx.search.limit)
	}
}

func TestSessionSyncError(t *testing.T) {
	tool, err := New(Config{}, Deps{Index: &stubIndex{syncErr: errors.New("sync fail")}})
	if err != nil {
		t.Fatal(err)
	}
	msg := callToolErrText(t, tool, `{"query":"hi"}`)
	if !strings.Contains(msg, "sync fail") {
		t.Fatalf("got %q", msg)
	}
}

func TestSessionUnknownMode(t *testing.T) {
	tool, err := New(Config{}, Deps{Index: &stubIndex{}})
	if err != nil {
		t.Fatal(err)
	}
	msg := callToolErrText(t, tool, `{"mode":"nope","query":"x"}`)
	if !strings.Contains(msg, "mode must be") {
		t.Fatalf("got %q", msg)
	}
}
