package acpremote

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	acp "github.com/coder/acp-go-sdk"
	"github.com/lengzhao/agentkit"
	capsession "github.com/lengzhao/agentkit/cap/session"
	"github.com/lengzhao/agentkit/runtime/loop"
	"github.com/lengzhao/agentkit/runtime/session/sessstore"
	"github.com/lengzhao/agentkit/testing/agenttest"
)

func TestSteeringCapability(t *testing.T) {
	for _, tc := range []struct {
		meta map[string]any
		want bool
	}{
		{nil, false}, {map[string]any{"steering": true}, false},
		{map[string]any{"steering": map[string]any{"supported": false}}, false},
		{map[string]any{"steering": map[string]any{"supported": true}}, true},
	} {
		if got := supportsSteering(tc.meta); got != tc.want {
			t.Fatalf("supportsSteering(%v)=%v", tc.meta, got)
		}
	}
}

func TestSteeringWorker(t *testing.T) {
	for _, outcome := range []string{"injected", "promptRequired", "methodNotFound", "invalid", "transportError"} {
		t.Run(outcome, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
			defer cancel()
			cr, sw := io.Pipe()
			sr, cw := io.Pipe()
			defer cr.Close()
			defer sw.Close()
			defer sr.Close()
			defer cw.Close()
			conn := acp.NewClientSideConnection(&bridgeClient{}, cw, cr)
			ctrl := loop.NewControl()
			defer ctrl.Stop()
			msg := agentkit.ModelMessage{Role: "user", Content: []agentkit.ContentPart{{Type: "text", Text: "change direction"}}}
			if err := ctrl.Steer(ctx, msg); err != nil {
				t.Fatal(err)
			}
			received := make(chan struct{})
			go func() {
				var req struct {
					ID     json.RawMessage   `json:"id"`
					Method string            `json:"method"`
					Params acp.PromptRequest `json:"params"`
				}
				if err := json.NewDecoder(sr).Decode(&req); err != nil {
					return
				}
				if req.Method != "_session/steering" || req.Params.SessionId != "remote" || len(req.Params.Prompt) != 1 || req.Params.Prompt[0].Text.Text != "change direction" {
					t.Errorf("unexpected request: %+v", req)
				}
				steer, _ := req.Params.Meta["steering"].(map[string]any)
				if steer["idleBehavior"] != "promptRequired" {
					t.Errorf("missing host-owned idle behavior: %v", req.Params.Meta)
				}
				if outcome == "transportError" {
					sw.Close()
					close(received)
					return
				}
				response := map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": map[string]any{"outcome": outcome}}
				if outcome == "methodNotFound" {
					delete(response, "result")
					response["error"] = map[string]any{"code": -32601, "message": "method not found"}
				}
				_ = json.NewEncoder(sw).Encode(response)
				close(received)
			}()
			stop := make(chan struct{})
			done := make(chan error, 1)
			accepted := 0
			go func() {
				done <- forwardSteering(ctx, stop, ctrl, conn, "remote", func() { _ = cw.Close() }, func(agentkit.ModelMessage) error { accepted++; return nil })
			}()
			select {
			case <-received:
			case <-ctx.Done():
				t.Fatal("no steering request")
			}
			close(stop)
			err := <-done
			if outcome == "invalid" || outcome == "transportError" {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if outcome == "injected" {
				if accepted != 1 || len(ctrl.PopSteering()) != 0 {
					t.Fatal("injected message not consumed exactly once")
				}
			} else {
				if accepted != 0 || len(ctrl.PopSteering()) != 1 {
					t.Fatal("rejected message not restored")
				}
			}
		})
	}
}

func TestRestoreSteeringPreservesOrder(t *testing.T) {
	ctrl := loop.NewControl()
	defer ctrl.Stop()
	msg := func(s string) agentkit.ModelMessage {
		return agentkit.ModelMessage{Role: "user", Content: []agentkit.ContentPart{{Type: "text", Text: s}}}
	}
	_ = ctrl.Steer(t.Context(), msg("first"))
	pending := ctrl.PopSteering()
	_ = ctrl.Steer(t.Context(), msg("second"))
	ctrl.RestoreSteering(pending)
	got := ctrl.PopSteering()
	if len(got) != 2 || got[0].Content[0].Text != "first" || got[1].Content[0].Text != "second" {
		t.Fatalf("order: %+v", got)
	}
}

// Test peer deliberately completes the prompt before acknowledging steering to
// exercise the race between prompt completion and delivery confirmation.
func TestACPSteeringHelper(t *testing.T) {
	mode := os.Getenv("AGENTKIT_STEERING_TEST")
	if mode == "" {
		return
	}
	decoder := json.NewDecoder(os.Stdin)
	encoder := json.NewEncoder(os.Stdout)
	var promptID json.RawMessage
	reply := func(id json.RawMessage, result any) {
		_ = encoder.Encode(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
	}
	for {
		var req struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		if decoder.Decode(&req) != nil {
			os.Exit(0)
		}
		switch req.Method {
		case "initialize":
			reply(req.ID, map[string]any{"protocolVersion": 1, "agentCapabilities": map[string]any{}, "_meta": map[string]any{"steering": map[string]any{"supported": mode != "unsupported"}}})
		case "session/new":
			result := map[string]any{"sessionId": "remote"}
			if mode == "model" {
				result["configOptions"] = []any{map[string]any{"id": "model", "type": "select", "currentValue": "remote-model", "options": []any{}}}
			}
			reply(req.ID, result)
		case "session/prompt":
			promptID = req.ID
			if mode == "stop" {
				_ = encoder.Encode(map[string]any{"jsonrpc": "2.0", "method": "session/update", "params": map[string]any{"sessionId": "remote", "update": map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]any{"type": "text", "text": "working"}}}})
			}
			if mode == "unsupported" || mode == "model" {
				reply(req.ID, map[string]any{"stopReason": "end_turn"})
			}
		case "session/cancel":
			if mode == "stop" {
				if err := os.WriteFile(os.Getenv("AGENTKIT_STOP_MARKER"), []byte("cancelled"), 0600); err != nil {
					os.Exit(1)
				}
				reply(promptID, map[string]any{"stopReason": "cancelled"})
			}
		case "_session/steering":
			reply(promptID, map[string]any{"stopReason": "end_turn"})
			time.Sleep(20 * time.Millisecond)
			outcome := mode
			if mode == "cancelOnAccept" {
				outcome = "injected"
			}
			reply(req.ID, map[string]any{"outcome": outcome})
		}
	}
}

func TestRunTurnStopCancelsRemotePrompt(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	marker := filepath.Join(dir, "cancelled")
	mem, err := sessstore.NewMemory(sessstore.MemoryConfig{ID: "stop-session"})
	if err != nil {
		t.Fatal(err)
	}
	ag, err := New(Config{ID: "acp", Command: []string{exe, "-test.run=^TestACPSteeringHelper$"}, Cwd: dir, Env: map[string]string{"AGENTKIT_STEERING_TEST": "stop", "AGENTKIT_STOP_MARKER": marker}}, Deps{Workspace: &stubWorkspace{}, FS: bindTestFS(t, dir), Telemetry: mustToolkit(t), SessionStore: &steeringTestStore{sess: mem}, SessionEvents: mustEvents(t)})
	if err != nil {
		t.Fatal(err)
	}
	defer ag.(*Runtime).bridgeFor("stop-session").releaseSubprocess()
	ctrl := loop.NewControl()
	defer ctrl.Stop()
	ctx, cancel := context.WithTimeout(agenttest.TurnContext("stop-session", "acp"), 2*time.Second)
	defer cancel()
	ctx = context.WithValue(ctx, agentkit.KeySessionControl, ctrl)
	stopped := false
	var remoteProc *subprocess
	var end capsession.TurnEndData
	err = ag.RunTurn(ctx, agentkit.TurnInput{Message: agentkit.ModelMessage{Role: "user", Content: []agentkit.ContentPart{{Type: "text", Text: "work"}}}, Emit: func(_ context.Context, ev agentkit.OutboundEvent) error {
		if ev.Type == agentkit.EventMessageUpdate && !stopped {
			stopped = true
			remoteProc = ag.(*Runtime).bridgeFor("stop-session").proc.Load()
			return ctrl.Cancel(context.Background(), "/stop")
		}
		if ev.Type == agentkit.EventTurnEnd {
			return json.Unmarshal(ev.Data, &end)
		}
		return nil
	}})
	if !stopped {
		t.Fatal("remote prompt did not start")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("stop error = %v, want context.Canceled", err)
	}
	if ctx.Err() != nil {
		t.Fatalf("stop waited for parent timeout: %v", ctx.Err())
	}
	if !end.Cancelled || end.StopReason != "/stop" {
		t.Fatalf("outbound turn end = %+v", end)
	}
	deadline := time.Now().Add(time.Second)
	for {
		if _, err := os.Stat(marker); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("remote peer did not receive session/cancel")
		}
		time.Sleep(time.Millisecond)
	}
	if remoteProc == nil || !remoteProc.alive() {
		t.Fatal("turn cancellation killed the reusable subprocess")
	}
	events, err := mem.Read(ctx, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range events {
		if event.Type == agentkit.EventTurnEnd {
			var persisted capsession.TurnEndData
			if err := json.Unmarshal(event.Data, &persisted); err != nil {
				t.Fatal(err)
			}
			if !persisted.Cancelled || persisted.StopReason != "/stop" {
				t.Fatalf("persisted turn end = %+v", persisted)
			}
			return
		}
	}
	t.Fatal("missing persisted turn end")
}

func TestRunTurnSteering(t *testing.T) {
	for _, mode := range []string{"injected", "promptRequired", "unsupported", "cancelOnAccept"} {
		t.Run(mode, func(t *testing.T) {
			exe, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			dir := t.TempDir()
			mem, err := sessstore.NewMemory(sessstore.MemoryConfig{ID: "steering-session"})
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(agenttest.TurnContext("steering-session", "acp"), 3*time.Second)
			defer cancel()
			store := &steeringTestStore{sess: mem}
			if mode == "cancelOnAccept" {
				store.cancelOnAccept = cancel
			}
			agent, err := New(Config{ID: "acp", Command: []string{exe, "-test.run=^TestACPSteeringHelper$"}, Cwd: dir, Env: map[string]string{"AGENTKIT_STEERING_TEST": mode}, ReleaseSubprocessAfterTurn: true}, Deps{Workspace: &stubWorkspace{}, FS: bindTestFS(t, dir), Telemetry: mustToolkit(t), SessionStore: store, SessionEvents: mustEvents(t)})
			if err != nil {
				t.Fatal(err)
			}
			ctrl := loop.NewControl()
			defer ctrl.Stop()
			ctx = context.WithValue(ctx, agentkit.KeySessionControl, ctrl)
			msg := agentkit.ModelMessage{Role: "user", Content: []agentkit.ContentPart{{Type: "text", Text: "steer"}}}
			_ = ctrl.Steer(ctx, msg)
			starts, ends := 0, 0
			err = agent.RunTurn(ctx, agentkit.TurnInput{Message: msg, Emit: func(_ context.Context, ev agentkit.OutboundEvent) error {
				if ev.Type == agentkit.EventTurnStart {
					starts++
				}
				if ev.Type == agentkit.EventTurnEnd {
					ends++
				}
				return nil
			}})
			if err != nil && !(mode == "cancelOnAccept" && errors.Is(err, context.Canceled)) {
				t.Fatal(err)
			}
			if starts != 1 || ends != 1 {
				t.Fatalf("lifecycle: starts=%d ends=%d", starts, ends)
			}
			events, err := mem.Read(ctx, 0)
			if err != nil {
				t.Fatal(err)
			}
			userMessages := 0
			for _, event := range events {
				if event.Type == agentkit.EventUserMessage {
					userMessages++
				}
			}
			wantUserMessages := 1
			if mode == "injected" || mode == "cancelOnAccept" {
				wantUserMessages = 2
			}
			if userMessages != wantUserMessages {
				t.Fatalf("persisted user messages=%d want=%d", userMessages, wantUserMessages)
			}
			pending := ctrl.PopSteering()
			want := 1
			if mode == "injected" || mode == "cancelOnAccept" {
				want = 0
			}
			if len(pending) != want {
				t.Fatalf("pending=%d want=%d", len(pending), want)
			}
		})
	}
}

func TestSteeringWorkerStopsWithoutConsumingQueue(t *testing.T) {
	ctrl := loop.NewControl()
	defer ctrl.Stop()
	_ = ctrl.Steer(t.Context(), agentkit.ModelMessage{Role: "user"})
	stop := make(chan struct{})
	close(stop)
	if err := forwardSteering(t.Context(), stop, ctrl, nil, "remote", func() {}, nil); err != nil {
		t.Fatal(err)
	}
	if len(ctrl.PopSteering()) != 1 {
		t.Fatal("stopped worker consumed pending message")
	}
}

func TestSteeringWorkerCancellation(t *testing.T) {
	ctrl := loop.NewControl()
	defer ctrl.Stop()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := forwardSteering(ctx, make(chan struct{}), ctrl, nil, "remote", func() {}, nil); err != context.Canceled {
		t.Fatalf("error=%v", err)
	}
}

func TestSteeringWriteCancellation(t *testing.T) {
	cr, sw := io.Pipe()
	sr, cw := io.Pipe()
	defer cr.Close()
	defer sw.Close()
	defer sr.Close()
	defer cw.Close()
	conn := acp.NewClientSideConnection(&bridgeClient{}, cw, cr)
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := callSteering(ctx, conn, "remote", agentkit.ModelMessage{Role: "user", Content: []agentkit.ContentPart{{Type: "text", Text: strings.Repeat("x", 1024*1024)}}}, func() { _ = cw.Close() })
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected cancelled write")
		}
	case <-time.After(time.Second):
		t.Fatal("steering write did not unblock")
	}
}

type steeringTestStore struct {
	sess           agentkit.Session
	gets           int
	cancelOnAccept context.CancelFunc
}

func (s *steeringTestStore) Get(ctx context.Context, _ agentkit.SessionID) (agentkit.Session, error) {
	s.gets++
	if s.gets == 3 && s.cancelOnAccept != nil {
		s.cancelOnAccept()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return s.sess, nil
}
