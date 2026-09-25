package agentkit

import (
	"context"
	"errors"
	"testing"
)

func TestIsTurnAbort(t *testing.T) {
	if IsTurnAbort(nil) {
		t.Fatal("nil is not turn abort")
	}
	if !IsTurnAbort(context.Canceled) {
		t.Fatal("context.Canceled is turn abort")
	}
	plain := errors.New("tool failed")
	if IsTurnAbort(plain) {
		t.Fatal("plain error is recoverable at agent")
	}
	if !IsTurnAbort(AbortTurn(plain)) {
		t.Fatal("AbortTurn wraps turn-fatal")
	}
}

func TestRecoverToolExecute(t *testing.T) {
	call := ToolCall{ID: "c1", Name: "demo"}
	res, err := RecoverToolExecute(call, ToolResult{}, errors.New("bad args"))
	if err != nil || res.Content != "bad args" {
		t.Fatalf("recoverable: res=%#v err=%v", res, err)
	}
	res, err = RecoverToolExecute(call, ToolResult{}, context.DeadlineExceeded)
	if err != nil || res.Audit["decision"] != "timeout" {
		t.Fatalf("timeout: res=%#v err=%v", res, err)
	}
	if IsTurnAbort(context.DeadlineExceeded) {
		t.Fatal("deadline exceeded must not abort turn")
	}
	_, err = RecoverToolExecute(call, ToolResult{}, AbortTurn(errors.New("hook")))
	if !IsTurnAbort(err) {
		t.Fatalf("abort: %v", err)
	}
}

func TestToolResultFromExecuteError(t *testing.T) {
	call := ToolCall{ID: "c1", Name: "demo"}
	res := ToolResultFromExecuteError(call, errors.New("invalid tool input"))
	if res.ID != "c1" || res.Audit["decision"] != "error" {
		t.Fatalf("result = %#v", res)
	}
	if res.Content != "invalid tool input" {
		t.Fatalf("content = %q", res.Content)
	}
}
