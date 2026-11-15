// Command renoxium runs the platform from one process: the Renoxium site
// (accounts, OAuth, builder MCP endpoint) and every app host.
package main

import (
	"context"
	"crypto/rand"
	"errors"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/arif-rachim/renoxium/internal/accounts"
	"github.com/arif-rachim/renoxium/internal/email"
	"github.com/arif-rachim/renoxium/internal/platform"
	"github.com/arif-rachim/renoxium/internal/sandbox"
	"github.com/arif-rachim/renoxium/internal/web"
	"github.com/arif-rachim/renoxium/internal/whatsapp"
)

func main() {
	// When started as a script worker, this process serves scripts and exits.
	sandbox.ServeWorkerIfRequested()

	addr := flag.String("addr", ":8080", "listen address")
	dataDir := flag.String("data", "data", "data directory")
	baseURL := flag.String("base-url", "", "public origin of the Renoxium site, e.g. https://renoxium.com; defaults to http://localhost<addr port>")
	appsDomain := flag.String("apps-domain", "localhost", "apps are served on <name>.<apps-domain>; use renoxium.app in production")
	trustedIPHeader := flag.String("trusted-ip-header", "", `header with the visitor address set by a local tunnel, e.g. "CF-Connecting-IP"; trusted only on loopback connections`)
	emailFrom := flag.String("email-from", "Renoxium <no-reply@renoxium.com>", "sender of account emails; its domain must be verified in Resend")
	workers := flag.Int("workers", 4, "number of script worker processes")
	flag.Parse()

	port := ""
	if i := strings.LastIndex(*addr, ":"); i >= 0 {
		port = (*addr)[i:]
	}
	if *baseURL == "" {
		*baseURL = "http://localhost" + port
	}
	var mailer email.Sender = email.Log{}
	if key := os.Getenv("RESEND_API_KEY"); key != "" {
		mailer = &email.Resend{Key: key, From: *emailFrom}
	} else {
		slog.Warn("RESEND_API_KEY is not set: emails are written to the log instead of being sent")
	}
	previewKey, err := loadKey(filepath.Join(*dataDir, "preview.key"))
	if err != nil {
		slog.Error("preview key", "err", err)
		os.Exit(1)
	}
	cfg := platform.Config{DataDir: *dataDir, AppsDomain: *appsDomain, PublicScheme: "https", Mailer: mailer, TrustedIPHeader: *trustedIPHeader,
		SiteURL: strings.TrimRight(*baseURL, "/"), PreviewKey: previewKey}
	if token, number, tmpl := os.Getenv("WHATSAPP_TOKEN"), os.Getenv("WHATSAPP_PHONE_NUMBER_ID"), os.Getenv("WHATSAPP_OTP_TEMPLATE"); token != "" && number != "" && tmpl != "" {
		cfg.WhatsApp = &whatsapp.CloudAPI{Token: token, PhoneNumberID: number, Template: tmpl,
			Language: os.Getenv("WHATSAPP_TEMPLATE_LANGUAGE"), Version: os.Getenv("WHATSAPP_API_VERSION")}
	} else {
		slog.Warn("WhatsApp is not configured: sign-in with WhatsApp works on previews only (codes in the app log)")
	}
	if *appsDomain == "localhost" {
		cfg.PublicScheme, cfg.PublicPort = "http", port
	}

	pool, err := sandbox.NewPool(sandbox.PoolConfig{Workers: *workers})
	if err != nil {
		slog.Error("start script workers", "err", err)
		os.Exit(1)
	}
	defer pool.Close()
	// accounts opens first so visitor emails can reply to the app owner.
	acc, err := accounts.Open(filepath.Join(*dataDir, "accounts.db"))
	if err != nil {
		slog.Error("open accounts", "err", err)
		os.Exit(1)
	}
	defer acc.Close()
	cfg.OwnerEmail = func(id string) (string, bool) {
		u, err := acc.User(id)
		return u.Email, err == nil && u.Verified
	}
	p, err := platform.New(cfg, pool)
	if err != nil {
		slog.Error("start platform", "err", err)
		os.Exit(1)
	}
	defer p.Close()
	site := web.New(*baseURL, acc, p)
	site.TrustedIPHeader = *trustedIPHeader
	site.Mailer = mailer

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !p.ServeApp(w, r) {
			site.ServeHTTP(w, r)
		}
	})
	go func() {
		for range time.Tick(time.Hour) {
			acc.Cleanup()
			p.Cleanup(context.Background())
		}
	}()
	srv := &http.Server{Addr: *addr, Handler: handler, ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 30 * time.Second}
	go func() {
		slog.Info("listening", "addr", *addr, "site", *baseURL, "mcp", *baseURL+"/mcp", "apps", "*."+*appsDomain)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("serve", "err", err)
			os.Exit(1)
		}
	}()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()
	shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	srv.Shutdown(shutdown)
}

// loadKey reads a 32-byte secret, creating it on first start. It signs
// preview links, so it never leaves the data directory.
func loadKey(path string) ([]byte, error) {
	if b, err := os.ReadFile(path); err == nil && len(b) == 32 {
		return b, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return nil, err
	}
	return b, os.WriteFile(path, b, 0o600)
}
