// Command renoxium runs the platform: app hosts, the builder MCP endpoint and
// a status page, all from one process.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/arif-rachim/renoxium/internal/mcpserver"
	"github.com/arif-rachim/renoxium/internal/platform"
	"github.com/arif-rachim/renoxium/internal/sandbox"
)

func main() {
	addr := flag.String("addr", ":8080", "listen address")
	dataDir := flag.String("data", "data", "data directory")
	appsDomain := flag.String("apps-domain", "localhost", "apps are served on <name>.<apps-domain>; use renoxium.app in production")
	scheme := flag.String("scheme", "http", "public URL scheme")
	publicPort := flag.String("public-port", "", `port in public URLs, e.g. ":8080"; defaults to the listen port when apps-domain is localhost`)
	flag.Parse()

	if *publicPort == "" && *appsDomain == "localhost" {
		if i := strings.LastIndex(*addr, ":"); i >= 0 {
			*publicPort = (*addr)[i:]
		}
	}
	token := os.Getenv("RENOXIUM_MCP_TOKEN")
	if token == "" {
		slog.Warn("RENOXIUM_MCP_TOKEN is not set: the builder MCP endpoint is open to anyone who can reach it")
	}

	p, err := platform.New(platform.Config{DataDir: *dataDir, AppsDomain: *appsDomain, PublicScheme: *scheme, PublicPort: *publicPort}, sandbox.NewGoja())
	if err != nil {
		slog.Error("start", "err", err)
		os.Exit(1)
	}
	defer p.Close()

	builder := mcpserver.Handler(mcpserver.New(p), token)
	mux := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if p.ServeApp(w, r) {
			return
		}
		switch r.URL.Path {
		case "/mcp":
			builder.ServeHTTP(w, r)
		case "/healthz":
			fmt.Fprintln(w, "ok")
		case "/":
			fmt.Fprintln(w, "Renoxium is running. Connect your agent to /mcp.")
		default:
			http.NotFound(w, r)
		}
	})

	srv := &http.Server{Addr: *addr, Handler: mux, ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 30 * time.Second}
	go func() {
		slog.Info("listening", "addr", *addr, "apps", "*."+*appsDomain)
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
