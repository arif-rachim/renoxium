// Command renoxium runs the platform from one process: the Renoxium site
// (accounts, OAuth, builder MCP endpoint) and every app host.
package main

import (
	"context"
	"crypto/rand"
	"errors"
	"flag"
	"fmt"
	"github.com/arif-rachim/renoxium/internal/licence"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/arif-rachim/renoxium/examples"
	"github.com/arif-rachim/renoxium/internal/accounts"
	"github.com/arif-rachim/renoxium/internal/cloudflare"
	"github.com/arif-rachim/renoxium/internal/email"
	"github.com/arif-rachim/renoxium/internal/monitor"
	"github.com/arif-rachim/renoxium/internal/platform"
	"github.com/arif-rachim/renoxium/internal/sandbox"
	"github.com/arif-rachim/renoxium/internal/seo"
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
	selfHosted := flag.Bool("self-hosted", false, "run the one app of a data directory made with import, on the owner's own server (docs/self-host.md)")
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "usage: renoxium [flags]                    serve\n"+
			"       renoxium [flags] suspend APP REASON  take an app offline (a running server notices within 30s)\n"+
			"       renoxium [flags] unsuspend APP       bring it back\n"+
			"       renoxium [flags] plan EMAIL PLAN     make an account a member by agreement (member) or remove that (free)\n"+
			"       renoxium [flags] reviewer EMAIL [APP] create or reset an account for a directory's reviewers, with an example store\n"+
			"       renoxium whatsapp-test NUMBER        send one sign-in code with the WHATSAPP_* settings and print Meta's answer\n"+
			"       renoxium shot-helper SOCKET          run Chromium for screenshots as a service of its own (deploy/renoxium-shots.service)\n"+
			"       renoxium version                     print the commit this binary was built from\n\n"+
			"on the owner's own server (docs/self-host.md):\n"+
			"       renoxium [flags] import EXPORT.zip ADMIN_EMAIL [YOUR_DOMAIN] [APP]  create a data directory from an export\n"+
			"       renoxium [flags] apply DEFINITION.json   check a changed definition and make it live\n"+
			"       renoxium [flags] rollback [VERSION]      make an earlier version live again\n"+
			"       renoxium [flags] admin-password EMAIL    give the admin account a new password\n"+
			"       renoxium licence-key                     make a key pair for signing exports' licences\n\nflags:\n")
		flag.PrintDefaults()
	}
	flag.Parse()
	if args := flag.Args(); len(args) > 0 {
		switch args[0] {
		case "whatsapp-test":
			os.Exit(whatsappTest(args[1:]))
		case "shot-helper":
			os.Exit(shotHelper(args[1:]))
		case "version":
			fmt.Println(buildVersion())
			os.Exit(0)
		}
		switch args[0] {
		case "reviewer":
			os.Exit(reviewerCommand(*dataDir, *appsDomain, args[1:]))
		case "import":
			os.Exit(importCommand(*dataDir, *appsDomain, args[1:]))
		case "apply":
			os.Exit(applyCommand(*dataDir, *appsDomain, args[1:]))
		case "rollback":
			os.Exit(rollbackCommand(*dataDir, *appsDomain, args[1:]))
		case "admin-password":
			os.Exit(adminPasswordCommand(*dataDir, args[1:]))
		case "licence-key":
			os.Exit(licenceKeyCommand())
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
		mailer = &email.Resend{Key: key, From: *emailFrom, Endpoint: os.Getenv("RESEND_API_URL")}
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
	// Screenshots of previews for building agents (screenshot_preview).
	// Opt-in: the hardened service needs a drop-in before Chromium can run
	// (docs/deploy-ubuntu.md, "Screenshots for agents").
	cfg.ChromiumSocket = os.Getenv("RENOXIUM_SHOT_SOCKET")
	switch noSandbox := os.Getenv("RENOXIUM_CHROMIUM_NO_SANDBOX"); {
	case cfg.ChromiumSocket != "":
		// Production: Chromium runs in the screenshot helper, never here.
		slog.Info("screenshots through the screenshot helper", "socket", cfg.ChromiumSocket)
	case os.Getenv("RENOXIUM_CHROMIUM") == "":
		slog.Info("RENOXIUM_SHOT_SOCKET is not set: agents cannot take screenshots of previews")
	case noSandbox == "dev":
		cfg.Chromium, cfg.ChromiumNoSandbox = os.Getenv("RENOXIUM_CHROMIUM"), true
		slog.Warn("RENOXIUM_CHROMIUM_NO_SANDBOX=dev: Chromium renders previews without its sandbox inside this process; never on a server with customers' data")
	case noSandbox != "":
		// Pages are written by app owners: an unsandboxed browser in this
		// process would put every app's data one browser bug away.
		slog.Error("RENOXIUM_CHROMIUM_NO_SANDBOX is set: screenshots are off. Run the screenshot helper instead (docs/deploy-ubuntu.md, \"Screenshots for agents\")")
	default:
		cfg.Chromium = os.Getenv("RENOXIUM_CHROMIUM")
		slog.Warn("Chromium for screenshots runs inside this process; the screenshot helper (RENOXIUM_SHOT_SOCKET) keeps it away from the apps' data")
	}
	cfg.SelfHosted = *selfHosted
	if seed := os.Getenv("RENOXIUM_LICENCE_KEY"); seed != "" && !*selfHosted {
		key, err := licence.KeyFromSeed(seed)
		if err != nil {
			slog.Error("RENOXIUM_LICENCE_KEY", "err", err)
			os.Exit(1)
		}
		cfg.LicenceKey = key
		slog.Info("exports from paid accounts carry a licence to run on the owner's server")
	}
	cfg.ScriptsPerOwner = max(1, *workers/2)
	cfg.ScriptsPerApp = cfg.ScriptsPerOwner
	if key := os.Getenv("STRIPE_SECRET_KEY"); key != "" {
		cfg.Stripe = &stripe.Client{Key: key, BaseURL: os.Getenv("STRIPE_API_BASE")}
		mode := "live"
		if strings.HasPrefix(key, "sk_test_") {
			mode = "test"
		}
		slog.Info("Stripe payments on", "mode", mode, "webhook_secret_set", os.Getenv("STRIPE_WEBHOOK_SECRET") != "")
	} else {
		slog.Warn("STRIPE_SECRET_KEY is not set: live apps cannot take card payments; previews simulate checkout")
	}
	if base := os.Getenv("FETCH_API_BASE"); base != "" {
		// End-to-end tests only: every http.fetch goes to <base>/<host>/<path>
		// instead of the internet. Never set this in production.
		target, err := url.Parse(base)
		if err != nil || target.Host == "" {
			slog.Error("FETCH_API_BASE is not a URL", "value", base)
			os.Exit(1)
		}
		cfg.FetchTransport = fetchTo{target}
		slog.Warn("FETCH_API_BASE is set: apps' http.fetch calls go to a test server, not the internet", "base", base)
	}
	if token, zone := os.Getenv("CLOUDFLARE_API_TOKEN"), os.Getenv("CLOUDFLARE_ZONE_ID"); token != "" && zone != "" {
		cfg.Cloudflare = &cloudflare.Client{Token: token, ZoneID: zone, BaseURL: os.Getenv("CLOUDFLARE_API_BASE")}
		cfg.CustomDomainTarget = os.Getenv("CUSTOM_DOMAIN_TARGET")
		cfg.LookupTXT = platform.LookupTXT(os.Getenv("RENOXIUM_DNS_API")) // default: Cloudflare's DNS over HTTPS
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
	cfg.OwnerPlan = acc.OwnerPlan
	cfg.WorkSeats = cfg.Stripe != nil && os.Getenv("STRIPE_PRICE_WORK_SEAT") != "" // extra staff of work apps can be paid for
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
	if *selfHosted {
		app, err := selfHostedApp(*dataDir, p)
		if err != nil {
			slog.Error("self-hosted", "err", err)
			os.Exit(1)
		}
		site.SingleApp = app
		slog.Info("self-hosted: one app on its owner's server", "app", app, "site", p.URL(app, platform.Live), "admin", *baseURL)
	}
	site.TrustedIPHeader = *trustedIPHeader
	site.Mailer = mailer
	site.Version = buildVersion()
	site.StripeWebhookSecret = os.Getenv("STRIPE_WEBHOOK_SECRET")
	site.Legal = web.Legal{Company: os.Getenv("RENOXIUM_LEGAL_NAME"), Address: os.Getenv("RENOXIUM_LEGAL_ADDRESS"),
		Email: os.Getenv("RENOXIUM_LEGAL_EMAIL"), Reviewed: os.Getenv("RENOXIUM_LEGAL_REVIEWED") == "1",
		WhatsApp: os.Getenv("RENOXIUM_WHATSAPP")}
	if social := strings.TrimSpace(os.Getenv("RENOXIUM_SOCIAL")); social != "" {
		site.Legal.Social = strings.Split(social, ",")
	}
	if ops := strings.TrimSpace(os.Getenv("RENOXIUM_OPERATORS")); ops != "" {
		site.Operators = strings.Split(ops, ",")
	}
	site.Billing = web.Billing{Stripe: cfg.Stripe,
		WebhookSecret: os.Getenv("STRIPE_BILLING_WEBHOOK_SECRET"), PriceWorkApp: os.Getenv("STRIPE_PRICE_WORK_APP"),
		PriceWorkSeat: os.Getenv("STRIPE_PRICE_WORK_SEAT")}
	if cfg.Stripe != nil {
		slog.Info("Renoxium billing", "work_app_price_set", site.Billing.PriceWorkApp != "", "work_seat_price_set", site.Billing.PriceWorkSeat != "",
			"billing_webhook_secret_set", site.Billing.WebhookSecret != "")
	}
	site.AbuseEmail = *abuseEmail
	site.OpenAIAppsChallenge = strings.TrimSpace(os.Getenv("OPENAI_APPS_CHALLENGE"))

	handler := seo.Compress(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !p.ServeApp(w, r) {
			site.ServeHTTP(w, r)
		}
	}))
	// Backups are checked once backup.sh is set up: its cron entry or its stamp exists.
	_, cronErr := os.Stat("/etc/cron.d/renoxium-backup")
	_, stampErr := os.Stat(filepath.Join(*dataDir, ".last-backup"))
	mon := &monitor.Monitor{Platform: p, Mailer: mailer, Operators: site.Operators, DataDir: *dataDir, SiteURL: *baseURL,
		Backups: cronErr == nil || stampErr == nil}
	site.Monitor = mon
	go mon.Run(context.Background())
	jobsEvery, _ := time.ParseDuration(os.Getenv("JOBS_CHECK_EVERY")) // default 30s
	go p.RunJobs(context.Background(), jobsEvery)
	go p.RunTraffic(context.Background(), time.Minute)
	go func() { // photos stored before uploads were shrunk; a one-off pass, a few a second at most
		if n := p.FitStoredPhotos(context.Background(), 300*time.Millisecond); n > 0 {
			slog.Info("stored photos made to fit", "files", n)
		}
	}()
	go p.RunUsage(context.Background()) // resource meter, per app and hour
	// Funnel events for steps owners took before they were recorded.
	if !*selfHosted {
		go func() {
			if err := p.BackfillFunnel(context.Background()); err != nil {
				slog.Error("backfill funnel events", "err", err)
			}
		}()
		// Template demos: one public app per gallery example, brought up to
		// date at every start and put back to seed data every hour.
		go func() {
			if err := p.EnsureDemos(context.Background(), examples.Gallery()); err != nil {
				slog.Error("template demos", "err", err)
			}
		}()
	}
	go func() {
		for range time.Tick(time.Hour) {
			acc.Cleanup()
			p.Cleanup(context.Background())
			if *selfHosted {
				continue
			}
			if err := p.ResetDemos(context.Background()); err != nil {
				slog.Error("reset template demos", "err", err)
			}
		}
	}()
	srv := &http.Server{Addr: *addr, Handler: handler, ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 30 * time.Second}
	srv.RegisterOnShutdown(p.StopLive) // realtime streams end at once; browsers reconnect
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

// fetchTo sends every request to one test server, keeping the original host
// as the first path segment.
type fetchTo struct{ base *url.URL }

func (f fetchTo) RoundTrip(r *http.Request) (*http.Response, error) {
	out := r.Clone(r.Context())
	out.URL.Scheme, out.URL.Host = f.base.Scheme, f.base.Host
	out.URL.Path = strings.TrimRight(f.base.Path, "/") + "/" + r.URL.Hostname() + r.URL.Path
	out.URL.RawPath = ""
	out.Host = f.base.Host
	return http.DefaultTransport.RoundTrip(out)
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
