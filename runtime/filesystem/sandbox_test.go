package filesystem

import (
	"context"
	"errors"
	"testing"

	capfs "github.com/lengzhao/agentkit/cap/filesystem"
	capsandbox "github.com/lengzhao/agentkit/cap/sandbox"
)

// recordingInner counts cap/filesystem.Service calls (cannot use testing/agenttest
// here: agenttest → sessstore → filesystem import cycle).
type recordingInner struct {
	readCalls   int
	statCalls   int
	listCalls   int
	writeCalls  int
	appendCalls int
	grepCalls   int
	findCalls   int
}

func (r *recordingInner) Read(context.Context, string) ([]byte, error) {
	r.readCalls++
	return []byte("ok"), nil
}

func (r *recordingInner) Stat(context.Context, string) (capfs.Info, error) {
	r.statCalls++
	return capfs.Info{}, nil
}

func (r *recordingInner) List(context.Context, string) ([]capfs.DirEntry, error) {
	r.listCalls++
	return nil, nil
}

func (r *recordingInner) Write(context.Context, string, []byte, ...capfs.WriteOption) error {
	r.writeCalls++
	return nil
}

func (r *recordingInner) Append(context.Context, string, []byte) error {
	r.appendCalls++
	return nil
}

func (r *recordingInner) Grep(context.Context, capfs.GrepRequest) (capfs.GrepResult, error) {
	r.grepCalls++
	return capfs.GrepResult{}, nil
}

func (r *recordingInner) Find(context.Context, capfs.FindRequest) (capfs.FindResult, error) {
	r.findCalls++
	return capfs.FindResult{}, nil
}

type gateSandbox struct {
	readErr  error
	writeErr error
}

func (g gateSandbox) Enabled() bool { return true }

func (g gateSandbox) WrapArgv(context.Context, string, []string) ([]string, error) {
	return nil, nil
}

func (g gateSandbox) CheckRead(context.Context, string) error  { return g.readErr }
func (g gateSandbox) CheckWrite(context.Context, string) error { return g.writeErr }

func TestNewSandboxedRequiresDeps(t *testing.T) {
	_, err := NewSandboxed(SandboxConfig{}, SandboxDeps{})
	if err == nil {
		t.Fatal("expected error without fs and sandbox")
	}
	_, err = NewSandboxed(SandboxConfig{}, SandboxDeps{FS: &recordingInner{}})
	if err == nil {
		t.Fatal("expected error without sandbox")
	}
}

func TestSandboxedDenySkipsInner(t *testing.T) {
	ctx := context.Background()
	path := "/tenant/work/a.go"
	inner := &recordingInner{}
	guard := gateSandbox{readErr: capsandbox.ErrDenied, writeErr: capsandbox.ErrDenied}
	svc, err := NewSandboxed(SandboxConfig{}, SandboxDeps{FS: inner, Sandbox: guard})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := svc.Read(ctx, path); !errors.Is(err, capsandbox.ErrDenied) {
		t.Fatalf("Read: %v", err)
	}
	if _, err := svc.Stat(ctx, path); !errors.Is(err, capsandbox.ErrDenied) {
		t.Fatalf("Stat: %v", err)
	}
	if _, err := svc.List(ctx, path); !errors.Is(err, capsandbox.ErrDenied) {
		t.Fatalf("List: %v", err)
	}
	if err := svc.Write(ctx, path, []byte("x")); !errors.Is(err, capsandbox.ErrDenied) {
		t.Fatalf("Write: %v", err)
	}
	if err := svc.Append(ctx, path, []byte("x")); !errors.Is(err, capsandbox.ErrDenied) {
		t.Fatalf("Append: %v", err)
	}
	if _, err := svc.Grep(ctx, capfs.GrepRequest{Path: path}); !errors.Is(err, capsandbox.ErrDenied) {
		t.Fatalf("Grep: %v", err)
	}
	if _, err := svc.Find(ctx, capfs.FindRequest{Path: path}); !errors.Is(err, capsandbox.ErrDenied) {
		t.Fatalf("Find: %v", err)
	}

	if inner.readCalls != 0 || inner.statCalls != 0 || inner.listCalls != 0 ||
		inner.writeCalls != 0 || inner.appendCalls != 0 || inner.grepCalls != 0 || inner.findCalls != 0 {
		t.Fatalf("inner must not be called on deny: %+v", inner)
	}
}

func TestSandboxedAllowCallsInner(t *testing.T) {
	ctx := context.Background()
	path := "/tenant/work/a.go"
	inner := &recordingInner{}
	svc, err := NewSandboxed(SandboxConfig{}, SandboxDeps{FS: inner, Sandbox: gateSandbox{}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Read(ctx, path); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Stat(ctx, path); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.List(ctx, path); err != nil {
		t.Fatal(err)
	}
	if err := svc.Write(ctx, path, []byte("x")); err != nil {
		t.Fatal(err)
	}
	if err := svc.Append(ctx, path, []byte("y")); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Grep(ctx, capfs.GrepRequest{Path: path}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Find(ctx, capfs.FindRequest{Path: path}); err != nil {
		t.Fatal(err)
	}
	if inner.readCalls != 1 || inner.statCalls != 1 || inner.listCalls != 1 ||
		inner.writeCalls != 1 || inner.appendCalls != 1 || inner.grepCalls != 1 || inner.findCalls != 1 {
		t.Fatalf("expected one inner call per method: %+v", inner)
	}
}
