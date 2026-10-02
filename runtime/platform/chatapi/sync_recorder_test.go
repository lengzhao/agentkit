package chatapi

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"sync"
)

// syncRecorder wraps httptest.ResponseRecorder with a mutex-guarded body so
// tests can poll SSE output while the handler goroutine is still writing
// (httptest.ResponseRecorder itself is not goroutine-safe).
type syncRecorder struct {
	rec *httptest.ResponseRecorder
	mu  sync.Mutex
	buf bytes.Buffer
}

func newSyncRecorder() *syncRecorder {
	return &syncRecorder{rec: httptest.NewRecorder()}
}

func (r *syncRecorder) Header() http.Header { return r.rec.Header() }

func (r *syncRecorder) WriteHeader(code int) { r.rec.WriteHeader(code) }

func (r *syncRecorder) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.buf.Write(p)
}

// Flush implements http.Flusher (required by newSSEWriter).
func (r *syncRecorder) Flush() {}

// Code mirrors httptest.ResponseRecorder.Code.
func (r *syncRecorder) Code() int { return r.rec.Code }

func (r *syncRecorder) bodyString() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.buf.String()
}

func (r *syncRecorder) bodyBytes() []byte {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]byte(nil), r.buf.Bytes()...)
}
