// Command renoxium runs the platform from one process: the Renoxium site
// (accounts, OAuth, builder MCP endpoint) and every app host.
package main

import (
	"context"
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
	"github.com/arif-rachim/renoxium/internal/platform"
	"github.com/arif-rachim/renoxium/internal/sandbox"
	"github.com/arif-rachim/renoxium/internal/web"
)

func main() {
	// When started as a script worker, this process serves scripts and exits.
	sandbox.ServeWorkerIfRequested()

	addr := flag.String("addr", ":8080", "listen address")
	dataDir := flag.String("data", "data", "data directory")
	baseURL := flag.String("base-url", "", "public origin of the Renoxium site, e.g. https://renoxium.com; defaults to http://localhost<addr port>")
	appsDomain := flag.String("apps-domain", "localhost", "apps are served on <name>.<apps-domain>; use renoxium.app in production")
	trustedIPHeader := flag.String("trusted-ip-header", "", `header with the visitor address set by a local tunnel, e.g. "CF-Connecting-IP"; trusted only on loopback connections`)
	workers := flag.Int("workers", 4, "number of script worker processes")
	flag.Parse()

	port := ""
	if i := strings.LastIndex(*addr, ":"); i >= 0 {
		port = (*addr)[i:]
	}
	if *baseURL == "" {
		*baseURL = "http://localhost" + port
	}
	cfg := platform.Config{DataDir: *dataDir, AppsDomain: *appsDomain, PublicScheme: "https"}
	if *appsDomain == "localhost" {
		cfg.PublicScheme, cfg.PublicPort = "http", port
	}

	pool, err := sandbox.NewPool(sandbox.PoolConfig{Workers: *workers})
	if err != nil {
		slog.Error("start script workers", "err", err)
		os.Exit(1)
	}
	defer pool.Close()
	p, err := platform.New(cfg, pool)
	if err != nil {
		slog.Error("start platform", "err", err)
		os.Exit(1)
	}
	defer p.Close()
	acc, err := accounts.Open(filepath.Join(*dataDir, "accounts.db"))
	if err != nil {
		slog.Error("open accounts", "err", err)
		os.Exit(1)
	}
	defer acc.Close()
	site := web.New(*baseURL, acc, p)
	site.TrustedIPHeader = *trustedIPHeader

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !p.ServeApp(w, r) {
			site.ServeHTTP(w, r)
		}
	})
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
