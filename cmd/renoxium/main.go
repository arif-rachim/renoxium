// Command renoxium runs the platform from one process: the Renoxium site
// (accounts, OAuth, builder MCP endpoint) and every app host.
package main

import (
	"context"
	"crypto/rand"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/arif-rachim/renoxium/internal/accounts"
	"github.com/arif-rachim/renoxium/internal/cloudflare"
	"github.com/arif-rachim/renoxium/internal/email"
	"github.com/arif-rachim/renoxium/internal/platform"
	"github.com/arif-rachim/renoxium/internal/sandbox"
	"github.com/arif-rachim/renoxium/internal/stripe"
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
	emailFrom := flag.String("email-from", "Renoxium <no-reply@renoxium.com>", "sender of account emails; its domain must be verified in Resend")
	workers := flag.Int("workers", 4, "number of script worker processes")
	requireSandbox := flag.Bool("require-sandbox", false, "refuse to run scripts when the kernel sandbox for script workers (Landlock) is unavailable")
	abuseEmail := flag.String("abuse-email", "", "address that receives abuse reports from /abuse; empty only logs them")
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "usage: renoxium [flags]                    serve\n"+
			"       renoxium [flags] suspend APP REASON  take an app offline (a running server notices within 30s)\n"+
			"       renoxium [flags] unsuspend APP       bring it back\n"+
			"       renoxium [flags] plan EMAIL PLAN     give an account the free, pro or business plan without payment\n"+
			"       renoxium whatsapp-test NUMBER        send one sign-in code with the WHATSAPP_* settings and print Meta's answer\n"+
			"       renoxium version                     print the commit this binary was built from\n\nflags:\n")
		flag.PrintDefaults()
	}
	flag.Parse()
	if args := flag.Args(); len(args) > 0 {
		switch args[0] {
		case "whatsapp-test":
			os.Exit(whatsappTest(args[1:]))
		case "version":
			fmt.Println(buildVersion())
			os.Exit(0)
		}
		os.Exit(adminCommand(*dataDir, args))
	}

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
	// One account may use at most half of the script workers, so other
	// accounts' apps always have a worker left.
	cfg.ScriptsPerOwner = max(1, *workers/2)
	cfg.ScriptsPerApp = cfg.ScriptsPerOwner
	if key := os.Getenv("STRIPE_SECRET_KEY"); key != "" {
		cfg.Stripe = &stripe.Client{Key: key}
		mode := "live"
		if strings.HasPrefix(key, "sk_test_") {
			mode = "test"
		}
		slog.Info("Stripe payments on", "mode", mode, "webhook_secret_set", os.Getenv("STRIPE_WEBHOOK_SECRET") != "")
	} else {
		slog.Warn("STRIPE_SECRET_KEY is not set: live apps cannot take card payments; previews simulate checkout")
	}
	if token, zone := os.Getenv("CLOUDFLARE_API_TOKEN"), os.Getenv("CLOUDFLARE_ZONE_ID"); token != "" && zone != "" {
		cfg.Cloudflare = &cloudflare.Client{Token: token, ZoneID: zone}
		cfg.CustomDomainTarget = os.Getenv("CUSTOM_DOMAIN_TARGET")
		slog.Info("custom domains on", "target", cfg.CustomDomainTarget)
	}
	if wa := whatsappFromEnv(); wa != nil {
		cfg.WhatsApp = wa
		slog.Info("WhatsApp sign-in codes on", "phone_number_id", wa.PhoneNumberID, "template", wa.Template, "language", wa.Language)
		if wa.NoticeTemplate != "" {
			cfg.WhatsAppNotices = wa
			slog.Info("WhatsApp owner notices on", "template", wa.NoticeTemplate)
		}
	} else {
		slog.Warn("WhatsApp is not configured: sign-in with WhatsApp works on previews only (codes in the app log)")
	}
	if *appsDomain == "localhost" {
		cfg.PublicScheme, cfg.PublicPort = "http", port
	}

	pool, err := sandbox.NewPool(sandbox.PoolConfig{Workers: *workers, RequireSandbox: *requireSandbox})
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
	cfg.OwnerPlan = acc.Plan
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
	site.Version = buildVersion()
	site.StripeWebhookSecret = os.Getenv("STRIPE_WEBHOOK_SECRET")
	site.Legal = web.Legal{Company: os.Getenv("RENOXIUM_LEGAL_NAME"), Address: os.Getenv("RENOXIUM_LEGAL_ADDRESS"),
		Email: os.Getenv("RENOXIUM_LEGAL_EMAIL"), Reviewed: os.Getenv("RENOXIUM_LEGAL_REVIEWED") == "1"}
	if ops := strings.TrimSpace(os.Getenv("RENOXIUM_OPERATORS")); ops != "" {
		site.Operators = strings.Split(ops, ",")
	}
	site.Billing = web.Billing{Stripe: cfg.Stripe, PricePro: os.Getenv("STRIPE_PRICE_PRO"), PriceBusiness: os.Getenv("STRIPE_PRICE_BUSINESS"),
		WebhookSecret: os.Getenv("STRIPE_BILLING_WEBHOOK_SECRET")}
	if cfg.Stripe != nil {
		slog.Info("Renoxium plans", "pro_price_set", site.Billing.PricePro != "", "business_price_set", site.Billing.PriceBusiness != "",
			"billing_webhook_secret_set", site.Billing.WebhookSecret != "")
	}
	site.AbuseEmail = *abuseEmail

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

// adminCommand runs an operator command against the data directory.
func adminCommand(dataDir string, args []string) int {
	if args[0] == "plan" {
		return planCommand(dataDir, args[1:])
	}
	if len(args) < 2 || (args[0] != "suspend" && args[0] != "unsuspend") {
		flag.Usage()
		return 2
	}
	p, err := platform.New(platform.Config{DataDir: dataDir}, nil)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer p.Close()
	if args[0] == "suspend" {
		err = p.Suspend(args[1], strings.Join(args[2:], " "))
	} else {
		err = p.Unsuspend(args[1])
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Printf("%sed %s\n", args[0], args[1])
	return 0
}
