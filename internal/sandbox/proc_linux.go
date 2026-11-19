package sandbox

import (
	"os/exec"
	"syscall"
	"time"
)

// setProcAttr makes workers die with the platform process.
func setProcAttr(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL}
}

// processCPU is this process's CPU time, user plus system. On Linux the
// sum comes from the scheduler's nanosecond runtime accounting, so it is
// accurate even for runs far shorter than a clock tick.
func processCPU() (time.Duration, bool) {
	var ru syscall.Rusage
	if syscall.Getrusage(syscall.RUSAGE_SELF, &ru) != nil {
		return 0, false
	}
	return time.Duration(ru.Utime.Nano() + ru.Stime.Nano()), true
}
