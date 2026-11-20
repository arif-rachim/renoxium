package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"os/user"
	"strconv"
	"syscall"

	"github.com/arif-rachim/renoxium/internal/platform"
)

// shotHelper runs the screenshot helper: Chromium for screenshot_preview,
// in a service and under a user of its own (deploy/renoxium-shots.service).
// It reads RENOXIUM_CHROMIUM (the browser), RENOXIUM_SHOT_USER (the only
// user that may connect; default renoxium, "*" for anyone the socket's file
// mode lets in) and RENOXIUM_CHROMIUM_NO_SANDBOX=1.
func shotHelper(args []string) int {
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, "usage: renoxium shot-helper SOCKET")
		return 2
	}
	if os.Geteuid() == 0 {
		fmt.Fprintln(os.Stderr, "shot-helper: must not run as root; give it a user of its own, without access to the data directory")
		return 2
	}
	h := platform.ShotHelper{Chromium: os.Getenv("RENOXIUM_CHROMIUM"), NoSandbox: os.Getenv("RENOXIUM_CHROMIUM_NO_SANDBOX") == "1", AllowUID: -1}
	if h.Chromium == "" {
		fmt.Fprintln(os.Stderr, "shot-helper: set RENOXIUM_CHROMIUM to the Chromium binary")
		return 2
	}
	who := os.Getenv("RENOXIUM_SHOT_USER")
	if who == "" {
		who = "renoxium"
	}
	if who != "*" {
		u, err := user.Lookup(who)
		if err != nil {
			fmt.Fprintln(os.Stderr, "shot-helper: RENOXIUM_SHOT_USER:", err)
			return 2
		}
		h.AllowUID, _ = strconv.Atoi(u.Uid)
	}
	os.Remove(args[0]) // left by an earlier run
	l, err := net.Listen("unix", args[0])
	if err != nil {
		fmt.Fprintln(os.Stderr, "shot-helper:", err)
		return 1
	}
	// Who may connect is decided by the peer check above, so the socket
	// itself is open to the platform's user, which is not in our group.
	if err := os.Chmod(args[0], 0o666); err != nil {
		fmt.Fprintln(os.Stderr, "shot-helper:", err)
		return 1
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	slog.Info("screenshot helper listening", "socket", args[0], "chromium", h.Chromium, "sandbox", !h.NoSandbox, "allowed_user", who)
	if err := h.Serve(ctx, l); err != nil {
		fmt.Fprintln(os.Stderr, "shot-helper:", err)
		return 1
	}
	return 0
}
