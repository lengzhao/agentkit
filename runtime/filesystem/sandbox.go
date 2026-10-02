package filesystem

import (
	"context"
	"fmt"

	capfs "github.com/lengzhao/agentkit/cap/filesystem"
	capsandbox "github.com/lengzhao/agentkit/cap/sandbox"
	"github.com/lengzhao/pluginkit"
)

// SandboxConfig configures filesystem/sandbox (the view config lives on the
// sandbox/bwrap instance).
type SandboxConfig struct{}

// SandboxDeps for filesystem/sandbox.
type SandboxDeps struct {
	FS      capfs.Service       `json:"fs"`
	Sandbox capsandbox.Service `json:"sandbox"`
}

type sandboxed struct {
	inner   capfs.Service
	sandbox capsandbox.Service
}

func init() {
	pluginkit.Register("filesystem/sandbox", NewSandboxed)
}

// NewSandboxed registers filesystem/sandbox: a cap/filesystem.Service
// decorator that enforces the sandbox/bwrap tenant view in-process
// (CheckRead/CheckWrite). bwrap only confines shell/ACP subprocesses; model
// file tools (tool/fs-workspace) run Go code inside the runner, so they go
// through this decorator to share the same view: neighbor tenants and masked
// paths are unreadable; only the tenant root and rwBinds are writable
// (everything else is denied, the in-process equivalent of EROFS).
func NewSandboxed(_ SandboxConfig, deps SandboxDeps) (capfs.Service, error) {
	if deps.FS == nil {
		return nil, fmt.Errorf("filesystem/sandbox requires fs (inner filesystem.Service)")
	}
	if deps.Sandbox == nil {
		return nil, fmt.Errorf("filesystem/sandbox requires sandbox (sandbox/bwrap instance)")
	}
	return &sandboxed{inner: deps.FS, sandbox: deps.Sandbox}, nil
}

func (g *sandboxed) Read(ctx context.Context, path string) ([]byte, error) {
	if err := g.sandbox.CheckRead(ctx, path); err != nil {
		return nil, err
	}
	return g.inner.Read(ctx, path)
}

func (g *sandboxed) Stat(ctx context.Context, path string) (capfs.Info, error) {
	if err := g.sandbox.CheckRead(ctx, path); err != nil {
		return capfs.Info{}, err
	}
	return g.inner.Stat(ctx, path)
}

func (g *sandboxed) List(ctx context.Context, path string) ([]capfs.DirEntry, error) {
	if err := g.sandbox.CheckRead(ctx, path); err != nil {
		return nil, err
	}
	return g.inner.List(ctx, path)
}

func (g *sandboxed) Write(ctx context.Context, path string, data []byte, opts ...capfs.WriteOption) error {
	if err := g.sandbox.CheckWrite(ctx, path); err != nil {
		return err
	}
	return g.inner.Write(ctx, path, data, opts...)
}

func (g *sandboxed) Append(ctx context.Context, path string, data []byte) error {
	if err := g.sandbox.CheckWrite(ctx, path); err != nil {
		return err
	}
	return g.inner.Append(ctx, path, data)
}

func (g *sandboxed) Grep(ctx context.Context, req capfs.GrepRequest) (capfs.GrepResult, error) {
	if err := g.sandbox.CheckRead(ctx, req.Path); err != nil {
		return capfs.GrepResult{}, err
	}
	return g.inner.Grep(ctx, req)
}

func (g *sandboxed) Find(ctx context.Context, req capfs.FindRequest) (capfs.FindResult, error) {
	if err := g.sandbox.CheckRead(ctx, req.Path); err != nil {
		return capfs.FindResult{}, err
	}
	return g.inner.Find(ctx, req)
}

var _ capfs.Service = (*sandboxed)(nil)
