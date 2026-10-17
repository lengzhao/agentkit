package acpclient

import (
	"context"
	"testing"

	acp "github.com/coder/acp-go-sdk"
	capacp "github.com/lengzhao/agentkit/cap/acp"
)

type fakeRestoreConn struct {
	resumeErr error
	loadErr   error
}

func (f *fakeRestoreConn) ResumeSession(context.Context, acp.ResumeSessionRequest) (acp.ResumeSessionResponse, error) {
	if f.resumeErr != nil {
		return acp.ResumeSessionResponse{}, f.resumeErr
	}
	return acp.ResumeSessionResponse{}, nil
}

func (f *fakeRestoreConn) LoadSession(context.Context, acp.LoadSessionRequest) (acp.LoadSessionResponse, error) {
	if f.loadErr != nil {
		return acp.LoadSessionResponse{}, f.loadErr
	}
	return acp.LoadSessionResponse{}, nil
}

func TestRestorerSkip(t *testing.T) {
	t.Parallel()
	r := NewRestorer(StrategySkip)
	ok, err := r.Restore(context.Background(), &fakeRestoreConn{}, capacp.SessionRestoreParams{})
	if err != nil || ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
}

func TestRestorerResumeSuccess(t *testing.T) {
	t.Parallel()
	r := NewRestorer(StrategyResume)
	ok, err := r.Restore(context.Background(), &fakeRestoreConn{}, capacp.SessionRestoreParams{ACPSessionID: "s1"})
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
}

func TestRestorerAutoPrefersResumeWhenAdvertised(t *testing.T) {
	t.Parallel()
	r := NewRestorer(StrategyAuto)
	caps := acp.AgentCapabilities{
		LoadSession: true,
		SessionCapabilities: acp.SessionCapabilities{
			Resume: &acp.SessionResumeCapabilities{},
		},
	}
	conn := &fakeRestoreConn{loadErr: acp.NewInvalidParams("should not call load")}
	ok, err := r.Restore(context.Background(), conn, capacp.SessionRestoreParams{
		ACPSessionID:      "s1",
		AgentCapabilities: caps,
	})
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
}

func TestRestorerCursorUsesLoad(t *testing.T) {
	t.Parallel()
	r := NewRestorer(StrategyCursor)
	conn := &fakeRestoreConn{resumeErr: acp.NewMethodNotFound(acp.AgentMethodSessionResume)}
	ok, err := r.Restore(context.Background(), conn, capacp.SessionRestoreParams{ACPSessionID: "s1"})
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
}

func TestRestorerAutoFallsBackToLoad(t *testing.T) {
	t.Parallel()
	r := NewRestorer(StrategyAuto)
	caps := acp.AgentCapabilities{LoadSession: true}
	ok, err := r.Restore(context.Background(), &fakeRestoreConn{}, capacp.SessionRestoreParams{
		ACPSessionID:      "s1",
		AgentCapabilities: caps,
	})
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
}
