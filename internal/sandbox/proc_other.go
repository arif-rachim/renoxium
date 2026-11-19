//go:build !linux

package sandbox

import (
	"os/exec"
	"time"
)

func setProcAttr(cmd *exec.Cmd) {}

// processCPU is not measured outside Linux; callers fall back to wall time.
func processCPU() (time.Duration, bool) { return 0, false }
