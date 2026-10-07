package subprocess

import "os/exec"

// PrepareExecCmd configures cancellation and subprocess cleanup (process group on Unix, WaitDelay).
func PrepareExecCmd(cmd *exec.Cmd) {
	prepareExecCmd(cmd)
}
