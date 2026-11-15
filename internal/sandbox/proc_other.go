//go:build !linux

package sandbox

import "os/exec"

func setProcAttr(cmd *exec.Cmd) {}
