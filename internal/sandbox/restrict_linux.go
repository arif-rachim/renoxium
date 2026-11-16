package sandbox

import (
	"errors"
	"fmt"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

// restrictWorker locks the worker process down with Landlock before it
// runs any tenant code: no file can be opened, no TCP connection made or
// accepted, and no signal sent to processes outside the worker. Scripts
// have no such API anyway; this holds even if the interpreter had a bug.
// The stdin and stdout pipes to the parent are already open and keep
// working.
func restrictWorker() error {
	// Load the local time zone now; it is read from a file on first use.
	_ = time.Local.String()

	abi, _, e := unix.Syscall(unix.SYS_LANDLOCK_CREATE_RULESET, 0, 0, unix.LANDLOCK_CREATE_RULESET_VERSION)
	if e != 0 {
		return fmt.Errorf("landlock is not available: %w", e)
	}
	var attr unix.LandlockRulesetAttr
	size := unsafe.Sizeof(attr.Handled_access_fs)
	attr.Handled_access_fs = 1<<13 - 1 // every file right of ABI 1
	if abi >= 2 {
		attr.Handled_access_fs |= unix.LANDLOCK_ACCESS_FS_REFER
	}
	if abi >= 3 {
		attr.Handled_access_fs |= unix.LANDLOCK_ACCESS_FS_TRUNCATE
	}
	if abi >= 5 {
		attr.Handled_access_fs |= unix.LANDLOCK_ACCESS_FS_IOCTL_DEV
	}
	if abi >= 4 {
		attr.Handled_access_net = unix.LANDLOCK_ACCESS_NET_BIND_TCP | unix.LANDLOCK_ACCESS_NET_CONNECT_TCP
		size += unsafe.Sizeof(attr.Handled_access_net)
	}
	if abi >= 6 {
		attr.Scoped = unix.LANDLOCK_SCOPE_ABSTRACT_UNIX_SOCKET | unix.LANDLOCK_SCOPE_SIGNAL
		size += unsafe.Sizeof(attr.Scoped)
	}
	fd, _, e := unix.Syscall(unix.SYS_LANDLOCK_CREATE_RULESET, uintptr(unsafe.Pointer(&attr)), size, 0)
	if e != 0 {
		return fmt.Errorf("create landlock ruleset: %w", e)
	}
	defer unix.Close(int(fd))
	// Both calls must reach every thread the Go runtime has started.
	// AllThreadsSyscall is unavailable in binaries built with cgo; release
	// builds use CGO_ENABLED=0.
	if _, _, e := syscall.AllThreadsSyscall(syscall.SYS_PRCTL, unix.PR_SET_NO_NEW_PRIVS, 1, 0); e != 0 {
		if errors.Is(e, syscall.ENOTSUP) {
			return errors.New("worker sandbox needs a binary built with CGO_ENABLED=0")
		}
		return fmt.Errorf("no_new_privs: %w", e)
	}
	if _, _, e := syscall.AllThreadsSyscall(unix.SYS_LANDLOCK_RESTRICT_SELF, fd, 0, 0); e != 0 {
		return fmt.Errorf("landlock restrict: %w", e)
	}
	// Few open files are needed: the pipes, and what the runtime holds.
	syscall.Setrlimit(syscall.RLIMIT_NOFILE, &syscall.Rlimit{Cur: 32, Max: 32})
	return nil
}
