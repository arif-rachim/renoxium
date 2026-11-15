package sandbox

import (
	"os/exec"
	"syscall"
)

// setProcAttr makes workers die with the platform process.
func setProcAttr(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL}
}
