package platform

import (
	"net"
	"syscall"
)

// peerUID returns the user at the other end of a unix socket connection.
func peerUID(conn net.Conn) (int, bool) {
	uc, ok := conn.(*net.UnixConn)
	if !ok {
		return -1, false
	}
	raw, err := uc.SyscallConn()
	if err != nil {
		return -1, false
	}
	uid, found := -1, false
	raw.Control(func(fd uintptr) {
		if cred, err := syscall.GetsockoptUcred(int(fd), syscall.SOL_SOCKET, syscall.SO_PEERCRED); err == nil {
			uid, found = int(cred.Uid), true
		}
	})
	return uid, found
}
