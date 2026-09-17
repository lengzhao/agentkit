package acpremote

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"

	acp "github.com/coder/acp-go-sdk"
)

// connLocalState is owned exclusively by connLoop (no mutex).
type connLocalState struct {
	proc         *subprocess
	promptCancel context.CancelFunc
	promptGen    uint64
}

type connOp interface {
	run(b *bridge, st *connLocalState)
}

type ensureConnOp struct {
	ctx  context.Context
	resp chan ensureConnResult
}

type ensureConnResult struct {
	proc *subprocess
	err  error
}

func (op ensureConnOp) run(b *bridge, st *connLocalState) {
	proc, err := b.connectSubprocess(op.ctx, st)
	if proc != nil {
		b.proc.Store(proc)
	} else {
		b.proc.Store(nil)
	}
	op.resp <- ensureConnResult{proc: proc, err: err}
}

type detachReleaseOp struct {
	done chan struct{}
}

func (op detachReleaseOp) run(b *bridge, st *connLocalState) {
	old, cancel := detachSubprocess(st)
	b.proc.Store(nil)
	go func() {
		terminateAndWaitSubprocess(old, cancel)
		close(op.done)
	}()
}

type subprocessExitOp struct {
	proc *subprocess
	err  error
}

func (op subprocessExitOp) run(b *bridge, st *connLocalState) {
	if st.proc != op.proc {
		return
	}
	st.proc = nil
	b.proc.Store(nil)
	cancel := st.promptCancel
	st.promptCancel = nil
	if cancel != nil {
		cancel()
	}
	if op.err != nil {
		slog.Warn("acp-remote: subprocess exited", "err", op.err)
	}
}

type beginPromptOp struct {
	ctx  context.Context
	resp chan beginPromptResult
}

type beginPromptResult struct {
	pctx context.Context
	gen  uint64
	end  func()
}

func (op beginPromptOp) run(b *bridge, st *connLocalState) {
	if st.promptCancel != nil {
		st.promptCancel()
	}
	st.promptGen++
	gen := st.promptGen
	pctx, cancel := context.WithCancel(op.ctx)
	st.promptCancel = cancel
	end := func() {
		b.connSend(endPromptOp{gen: gen})
		cancel()
	}
	op.resp <- beginPromptResult{pctx: pctx, gen: gen, end: end}
}

type endPromptOp struct {
	gen uint64
}

func (op endPromptOp) run(_ *bridge, st *connLocalState) {
	if st.promptGen == op.gen {
		st.promptCancel = nil
	}
}

func (b *bridge) connSend(op connOp) {
	b.connOps <- op
}

func (b *bridge) connLoop() {
	var st connLocalState
	for op := range b.connOps {
		op.run(b, &st)
	}
	// connOps closed: retire sessionState owner goroutines so they do not leak.
	if st.proc != nil {
		st.proc.states.Range(func(_, v any) bool {
			if s, ok := v.(*sessionState); ok {
				s.Stop()
			}
			return true
		})
	}
}

// stop closes connOps, retiring the connLoop goroutine. Call after Close (or
// dropBridge) has detached the subprocess. Safe to call once.
func (b *bridge) stop() {
	close(b.connOps)
}

// adoptProcOp seeds conn-loop state (tests).
type adoptProcOp struct {
	proc *subprocess
	done chan struct{}
}

func (op adoptProcOp) run(b *bridge, st *connLocalState) {
	st.proc = op.proc
	b.proc.Store(op.proc)
	close(op.done)
}

func detachSubprocess(st *connLocalState) (*subprocess, context.CancelFunc) {
	proc := st.proc
	if proc == nil {
		return nil, nil
	}
	st.proc = nil
	var cancel context.CancelFunc
	if st.promptCancel != nil {
		cancel = st.promptCancel
		st.promptCancel = nil
	}
	return proc, cancel
}

func (b *bridge) ensureConn(ctx context.Context) (*subprocess, error) {
	resp := make(chan ensureConnResult, 1)
	b.connSend(ensureConnOp{ctx: ctx, resp: resp})
	r := <-resp
	return r.proc, r.err
}

func (b *bridge) connectSubprocess(ctx context.Context, st *connLocalState) (*subprocess, error) {
	for {
		if st.proc != nil && st.proc.alive() && (st.proc.authenticated || b.cfg.AuthMethod == "") {
			return st.proc, nil
		}
		if st.proc != nil {
			old, cancel := detachSubprocess(st)
			b.proc.Store(nil)
			terminateAndWaitSubprocess(old, cancel)
		}

		cwd, err := b.resolveCwd(ctx)
		if err != nil {
			return nil, err
		}

		cmd := exec.CommandContext(ctx, b.cfg.Command[0], b.cfg.Command[1:]...)
		cmd.Dir = cwd
		configureCmdProcessGroup(cmd)
		cmd.Env = commandEnv(b.cfg.Env)
		cmd.Stderr = os.Stderr

		stdin, err := cmd.StdinPipe()
		if err != nil {
			return nil, fmt.Errorf("acp stdin pipe: %w", err)
		}
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			return nil, fmt.Errorf("acp stdout pipe: %w", err)
		}
		if err := cmd.Start(); err != nil {
			return nil, fmt.Errorf("acp start %v: %w", b.cfg.Command, err)
		}

		client := &bridgeClient{bridge: b}
		conn := acp.NewClientSideConnection(client, stdin, stdout)
		conn.SetLogger(slog.Default())

		initResp, err := conn.Initialize(ctx, acp.InitializeRequest{
			ProtocolVersion: acp.ProtocolVersionNumber,
			ClientCapabilities: acp.ClientCapabilities{
				Fs: acp.FileSystemCapabilities{
					ReadTextFile:  true,
					WriteTextFile: true,
				},
			},
			ClientInfo: &acp.Implementation{
				Name:    b.cfg.ClientName,
				Version: b.cfg.ClientVersion,
			},
		})
		if err != nil {
			terminateProcessGroup(cmd)
			_ = cmd.Wait()
			return nil, fmt.Errorf("acp initialize: %w", err)
		}
		slog.Info("acp-remote: connected", "protocol", initResp.ProtocolVersion)

		done := make(chan struct{})
		proc := &subprocess{cmd: cmd, conn: conn, client: client, done: done}
		st.proc = proc
		go b.waitSubprocess(proc)
		if err := b.authenticateSubprocess(ctx, proc); err != nil {
			old, cancel := detachSubprocess(st)
			terminateAndWaitSubprocess(old, cancel)
			return nil, err
		}
		return proc, nil
	}
}

func (b *bridge) authenticateSubprocess(ctx context.Context, proc *subprocess) error {
	if b.cfg.AuthMethod == "" {
		proc.authenticated = true
		return nil
	}
	if _, err := proc.conn.Authenticate(ctx, acp.AuthenticateRequest{
		MethodId: b.cfg.AuthMethod,
	}); err != nil {
		return fmt.Errorf("acp authenticate: %w (ensure cursor cli is logged in via agent login)", err)
	}
	proc.authenticated = true
	return nil
}

func (b *bridge) releaseSubprocess() {
	done := make(chan struct{})
	b.connSend(detachReleaseOp{done: done})
	<-done
}

func (b *bridge) beginPromptContext(ctx context.Context) (context.Context, func()) {
	resp := make(chan beginPromptResult, 1)
	b.connSend(beginPromptOp{ctx: ctx, resp: resp})
	r := <-resp
	return r.pctx, r.end
}

func (b *bridge) Close() error {
	done := make(chan struct{})
	b.connSend(detachReleaseOp{done: done})
	<-done
	return nil
}
