//go:build !linux

package platform

import "net"

// peerUID is only known on Linux; elsewhere a helper that names a user
// refuses every connection.
func peerUID(net.Conn) (int, bool) { return -1, false }
