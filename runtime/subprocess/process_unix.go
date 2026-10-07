//go:build unix

package subprocess

import (
	"os/exec"
	"syscall"
	"time"
)

const subprocessWaitDelay = 10 * time.Second

func prepareExecCmd(cmd *exec.Cmd) {
	if cmd == nil {
		return
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.WaitDelay = subprocessWaitDelay
	cmd.Cancel = func() error {
		terminateProcessGroup(cmd)
		return nil
	}
}

func terminateProcessGroup(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	pgid, err := syscall.Getpgid(cmd.Process.Pid)
	if err != nil {
		_ = cmd.Process.Kill()
		return
	}
	_ = syscall.Kill(-pgid, syscall.SIGTERM)
	time.Sleep(200 * time.Millisecond)
	_ = syscall.Kill(-pgid, syscall.SIGKILL)
}
