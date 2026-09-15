//go:build !unix

package acpremote

import "os/exec"

func configureCmdProcessGroup(cmd *exec.Cmd) {}

func terminateProcessGroup(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	_ = cmd.Process.Kill()
}
