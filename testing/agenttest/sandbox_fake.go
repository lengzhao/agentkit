package agenttest

import (
	"context"
	"slices"
	"sync"

	capsandbox "github.com/lengzhao/agentkit/cap/sandbox"
)

// SandboxWrapCall records one WrapArgv invocation.
type SandboxWrapCall struct {
	Cwd   string
	Inner []string
}

// RecordingSandbox implements capsandbox.Service and records WrapArgv calls.
type RecordingSandbox struct {
	mu sync.Mutex

	WrapErr     error
	WrappedArgv []string
	Calls       []SandboxWrapCall

	ReadErr  error
	WriteErr error
}

func (r *RecordingSandbox) Enabled() bool { return true }

func (r *RecordingSandbox) WrapArgv(_ context.Context, cwd string, inner []string) ([]string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.Calls = append(r.Calls, SandboxWrapCall{
		Cwd:   cwd,
		Inner: slices.Clone(inner),
	})
	if r.WrapErr != nil {
		return nil, r.WrapErr
	}
	if r.WrappedArgv != nil {
		return slices.Clone(r.WrappedArgv), nil
	}
	return slices.Clone(inner), nil
}

func (r *RecordingSandbox) CheckRead(context.Context, string) error  { return r.ReadErr }
func (r *RecordingSandbox) CheckWrite(context.Context, string) error { return r.WriteErr }

// LastWrapCall returns the most recent WrapArgv call, if any.
func (r *RecordingSandbox) LastWrapCall() (SandboxWrapCall, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.Calls) == 0 {
		return SandboxWrapCall{}, false
	}
	return r.Calls[len(r.Calls)-1], true
}

var _ capsandbox.Service = (*RecordingSandbox)(nil)

// GateSandbox returns fixed CheckRead/CheckWrite errors (nil allows).
type GateSandbox struct {
	ReadErr  error
	WriteErr error
}

func (g GateSandbox) Enabled() bool { return true }

func (g GateSandbox) WrapArgv(_ context.Context, _ string, inner []string) ([]string, error) {
	return slices.Clone(inner), nil
}

func (g GateSandbox) CheckRead(context.Context, string) error  { return g.ReadErr }
func (g GateSandbox) CheckWrite(context.Context, string) error { return g.WriteErr }

var _ capsandbox.Service = GateSandbox{}
