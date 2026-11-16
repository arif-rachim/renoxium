package sandbox

import (
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
)

// restrictProbe runs in a child process: it applies the worker sandbox and
// then tries what tenant code must never do.
func restrictProbe() {
	if err := restrictWorker(); err != nil {
		os.Stdout.WriteString("unavailable: " + err.Error())
		os.Exit(0)
	}
	var out []string
	if f, err := os.Open("/etc/hostname"); err == nil {
		f.Close()
		out = append(out, "read a file")
	}
	if err := os.WriteFile(os.TempDir()+"/rx-probe", []byte("x"), 0o600); err == nil {
		out = append(out, "wrote a file")
	}
	if fd, err := syscall.Socket(syscall.AF_INET, syscall.SOCK_STREAM, 0); err == nil {
		err := syscall.Connect(fd, &syscall.SockaddrInet4{Port: 1, Addr: [4]byte{127, 0, 0, 1}})
		if err != syscall.EACCES {
			out = append(out, "connect was not blocked: "+errString(err))
		}
		syscall.Close(fd)
	}
	os.Stdout.WriteString("done: " + strings.Join(out, ", "))
	os.Exit(0)
}

func errString(err error) string {
	if err == nil {
		return "connected"
	}
	return err.Error()
}

func TestWorkerSandboxBlocksFilesAndNetwork(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=^$")
	cmd.Env = append(os.Environ(), "RX_RESTRICT_PROBE=1")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("probe: %v %s", err, out)
	}
	got := string(out)
	if strings.HasPrefix(got, "unavailable: ") {
		t.Skip(got, "(run with CGO_ENABLED=0 on Linux 5.13 or newer)")
	}
	if got != "done: " {
		t.Fatalf("the sandboxed worker could still: %s", strings.TrimPrefix(got, "done: "))
	}
}
