package platform

import (
	"context"
	"io"
	"log/slog"
	"net"
	"os"
	"time"
)

// The screenshot helper runs Chromium for screenshot_preview in a process
// and under a user of its own, so a browser that a page manages to break
// out of finds none of the apps' data. The platform connects to its unix
// socket; each connection gets a fresh Chromium, and the bytes on the
// connection are the browser's DevTools pipe. The helper decides how
// Chromium is started; the platform still answers every request the
// browser makes (screenshot.go).

const (
	// helperShotTime is the longest one connection may keep its browser.
	helperShotTime = browserStartTimeout + 2*shotTimeout
	// helperBrowsers is how many browsers the helper runs at once.
	helperBrowsers = 2 * shotsAtOnce
)

// ShotHelper is the configuration of a screenshot helper.
type ShotHelper struct {
	Chromium  string // path of the Chromium or Chrome binary
	NoSandbox bool   // start Chromium without its own sandbox (where user namespaces are not allowed)
	// AllowUID is the only user that may connect, checked from the socket's
	// peer credentials; negative accepts anyone the socket's file mode lets in.
	AllowUID int
}

// Serve accepts connections until ctx ends or the listener fails.
func (h ShotHelper) Serve(ctx context.Context, l net.Listener) error {
	if _, err := os.Stat(h.Chromium); err != nil {
		return err
	}
	go func() {
		<-ctx.Done()
		l.Close()
	}()
	slots := make(chan struct{}, helperBrowsers)
	for {
		conn, err := l.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		if h.AllowUID >= 0 {
			if uid, ok := peerUID(conn); !ok || uid != h.AllowUID {
				slog.Warn("screenshot helper: connection refused", "peer_uid", uid)
				conn.Close()
				continue
			}
		}
		select {
		case slots <- struct{}{}:
		default:
			conn.Close() // busy: the platform never asks for this many
			continue
		}
		go func() {
			defer func() { <-slots }()
			h.serve(ctx, conn)
		}()
	}
}

func (h ShotHelper) serve(ctx context.Context, conn net.Conn) {
	defer conn.Close()
	ctx, cancel := context.WithTimeout(ctx, helperShotTime)
	defer cancel()
	c, err := spawnChromium(ctx, h.Chromium, h.NoSandbox)
	if err != nil {
		slog.Error("screenshot helper", "err", err)
		return
	}
	defer c.stop()
	done := make(chan struct{}, 2)
	go func() { io.Copy(c.in, conn); done <- struct{}{} }()
	go func() { io.Copy(conn, c.out); done <- struct{}{} }()
	select {
	case <-done:
	case <-ctx.Done():
	}
	// A browser that stopped by itself says why on its error output.
	select {
	case <-time.After(100 * time.Millisecond):
	case <-ctx.Done():
	}
	if t := c.stderr.String(); t != "" && ctx.Err() == nil {
		slog.Info("screenshot helper: Chromium stopped", "stderr", t)
	}
}
