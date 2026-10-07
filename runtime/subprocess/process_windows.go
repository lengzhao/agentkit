//go:build windows

package subprocess

import (
	"os/exec"
	"time"
)

const subprocessWaitDelay = 10 * time.Second

func prepareExecCmd(cmd *exec.Cmd) {
	if cmd == nil {
		return
	}
	cmd.WaitDelay = subprocessWaitDelay
	cmd.Cancel = func() error {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		return nil
	}
}
