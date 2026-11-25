// Package platform is the Renoxium engine: it stores apps and their versions,
// builds the preview and live stages, and knows which app a host belongs to.
package platform

import (
	"context"
	"crypto/ed25519"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	_ "modernc.org/sqlite"

	"github.com/arif-rachim/renoxium/internal/appdef"
	"github.com/arif-rachim/renoxium/internal/applog"
	"github.com/arif-rachim/renoxium/internal/cloudflare"
	"github.com/arif-rachim/renoxium/internal/email"
	"github.com/arif-rachim/renoxium/internal/files"
	"github.com/arif-rachim/renoxium/internal/sandbox"
	"github.com/arif-rachim/renoxium/internal/stripe"
	"github.com/arif-rachim/renoxium/internal/tenantdb"
	"github.com/arif-rachim/renoxium/internal/whatsapp"
)

// Stage names.
const (
	Live    = "live"
	Preview = "preview"
)

// Config locates data and describes how apps are reached from outside.
type Config struct {
	DataDir      string // where platform.db and per-app files live
	AppsDomain   string // apps are served on <name>.<AppsDomain>
	PublicScheme string // "https" in production
	PublicPort   string // e.g. ":8080" in development, "" in production
	// ScriptsPerApp caps how many scripts of one app run at the same time,
	// so one app cannot occupy every script worker. Default 2.
	ScriptsPerApp int
	// ScriptsPerOwner caps the scripts of all apps of one account. Default
	// ScriptsPerApp. Keep it below the number of script workers.
	ScriptsPerOwner int
	// Mailer sends visitor emails (confirmation, password reset) from live.
	Mailer email.Sender
	// TrustedIPHeader carries the visitor address behind a local tunnel.
	TrustedIPHeader string
	// Cloudflare sets up custom domains (Cloudflare for SaaS) on the apps
	// zone; CustomDomainTarget is the fallback origin owners point a CNAME at.
	Cloudflare         *cloudflare.Client
	CustomDomainTarget string
	// LookupTXT finds a name's TXT records, to check that a custom domain
	// belongs to the account adding it. Nil uses LookupTXT("").
	LookupTXT func(ctx context.Context, name string) ([]string, error)
	// Stripe takes payments for live apps on their owners' Stripe
	// accounts. Nil turns live checkout off; previews always simulate it.
	Stripe *stripe.Client
	// WhatsApp sends sign-in codes from live. Nil turns phone sign-in off
	// on live; preview always offers it, with codes in the app log.
	WhatsApp whatsapp.Sender
	// WhatsAppNotices sends notify.owner notices to owners who confirmed a
	// number. Nil turns that off.
	WhatsAppNotices whatsapp.Notifier
	// FetchTransport replaces http.fetch's own transport, which refuses
	// private addresses. Tests only.
	FetchTransport http.RoundTripper
	// SelfHosted is set when one app runs on its owner's own server: payments
	// go to the Stripe key's own account (stripe.DirectAccount), without
	// Connect and without a platform fee.
	SelfHosted bool
	// WorkSeats is set when extra staff members of a work app can be paid
	// for (WorkSeatPriceAED each); without it a work app has WorkAppUsers.
	WorkSeats bool
	// LicenceKey signs the licence an export from a paid account carries,
	// which lets it run on its owner's server (internal/licence). Nil: exports
	// carry none.
	LicenceKey ed25519.PrivateKey

	// OwnerPlan returns an account's plan: "free", "member" (a card saved)
	// or "granted" (a member by agreement with Renoxium). On free and member
	// accounts each app is a static site, a shop (a cut of card payments) or
	// a work app (a subscription); a free account may have AppsPerPlan
	// "free" apps, a member MaxAppsPerOwner. Nil means no plans: nothing is
	// limited.
	OwnerPlan func(ownerID string) string
	// OwnerEmail returns an app owner's verified email address, used as the
	// reply-to address of visitor emails. ok is false when there is none.
	OwnerEmail func(ownerID string) (address string, ok bool)
	// Daily caps messages that cost money or sender reputation. Each send
	// is also limited per visitor address and per recipient. Zero fields
	// take the defaults in DefaultDaily.
	Daily DailyLimits
	// SiteURL is the public origin of the Renoxium site, e.g.
	// https://renoxium.com, where owners open their private previews.
	SiteURL string
	// PreviewKey signs preview access. When set, previews are private:
	// only a browser that opened a link from the owner's dashboard sees
	// them. Empty leaves previews open (development and tests).
	PreviewKey []byte
	// Chromium is the path of a Chromium or Chrome binary for screenshots
	// of previews (screenshot_preview). Empty turns screenshots off.
	Chromium string
	// ChromiumNoSandbox allows a Chromium started by this process to run
	// without its own sandbox (always needed as root). Only for tests and
	// development machines: the browser renders pages written by app
	// owners, in a process that holds every app's data.
	ChromiumNoSandbox bool
	// ChromiumSocket is the unix socket of the screenshot helper
	// (renoxium shot-helper), which runs Chromium as a user of its own.
	// When set, Chromium is never started in this process.
	ChromiumSocket string
}

// DailyLimits caps messages sent per day, per app and over all apps.
type DailyLimits struct {
	EmailsPerApp, Emails, WhatsAppPerApp, WhatsApp int
}

// DefaultDaily are the limits used for zero fields of Config.Daily.
var DefaultDaily = DailyLimits{EmailsPerApp: 300, Emails: 3000, WhatsAppPerApp: 100, WhatsApp: 1000}

// Platform is safe for concurrent use.
type Platform struct {
	cfg   Config
	db    *sql.DB
	sb    sandbox.Sandbox
	files *files.Store

	change sync.Mutex // serialises ApplyChange and Publish

	mu     sync.Mutex
	stages map[string]*stage
	logs   map[string]*applog.Log
	bg     sync.WaitGroup // work that continues after a request is answered
	hosts  sync.Map       // host -> hostEntry
	owners sync.Map       // app -> owner
	lapsed sync.Map       // app -> lapsedState (see planLapsed)
	// liveHosts maps an app to its active custom domain.
	liveHosts sync.Map
	errors    errorCounter
	jobs      jobCache
	live      liveHub
	intake    map[string]*window
	slots     map[string]chan struct{} // per-app script concurrency
	limits    map[string]*window       // visitor sign-in rate limits
	rids      map[string]*ridEntry     // request IDs of served pages

	suspendedAt time.Time       // when suspended was last read
	suspended   map[string]bool // apps taken offline

	adminNotices sync.Map // app + "\x00" + email -> failed on_admin_change notice

	sitemaps sitemapCache  // public pages per stage (seo.go)
	demoBox  demoInbox     // messages template demos would have sent (demo.go)
	traffic  trafficBuffer // visits counted in memory until the next write (traffic.go)
	demoURLs sync.Map      // "app/file" -> address of a demo photo already uploaded (demodata.go)

	funnelSeen sync.Map // owner + "\x00" + kind -> first event already recorded (growth.go)

	shots    chan struct{} // browsers running for screenshots (screenshot.go)
	testJars sync.Map      // app + "\x00" + session -> *testJar, test_request cookie jars (testsession.go)

	meter           meter                       // resources used per app stage and hour (meter.go)
	throttles       atomic.Pointer[throttleSet] // operator throttles (throttle.go)
	throttleLoading atomic.Bool
}

type stage struct {
	app     string
	name    string
	version int
	def     *appdef.App
	tmpl    *template.Template
	db      *tenantdb.DB
	admin   *sql.DB // platform access to rx_ tables; never given to scripts
}

type hostEntry struct{ app, stage string }

// AppInfo describes an app to the building agent.
type AppInfo struct {
	App            string `json:"app"`
	LiveVersion    int    `json:"live_version" jsonschema:"version currently served on live_url; 0 means not published yet"`
	PreviewVersion int    `json:"preview_version" jsonschema:"version currently served on preview_url; 0 means nothing applied yet"`
	LiveURL        string `json:"live_url"`
	PreviewURL     string `json:"preview_url" jsonschema:"opens the private preview for the owner; add a page path to open that page, e.g. <preview_url>/queue"`
}

// Check is the result of rendering one route during ApplyChange.
type Check struct {
	Route    string   `json:"route"`
	Path     string   `json:"path,omitempty" jsonschema:"the path rendered, with its parameters filled from the first row of the matching table"`
	As       string   `json:"as,omitempty" jsonschema:"who the page was rendered for, for routes that need a signed-in visitor"`
	Status   int      `json:"status"`
	Error    string   `json:"error,omitempty" jsonschema:"template or script error, with the template name and line"`
	Warnings []string `json:"warnings,omitempty" jsonschema:"problems in a page that rendered, such as <no value> from a missing template key"`
	Note     string   `json:"note,omitempty"`
}

// ApplyResult reports what ApplyChange did.
type ApplyResult struct {
	OK         bool     `json:"ok" jsonschema:"false when the definition was rejected; see problems"`
	Problems   []string `json:"problems,omitempty" jsonschema:"why the definition was rejected; fix these and call apply_change again"`
	Version    int      `json:"version,omitempty"`
	PreviewURL string   `json:"preview_url,omitempty"`
	Migrations []string `json:"migrations,omitempty" jsonschema:"schema changes applied to the preview database"`
	Fixed      []string `json:"fixed,omitempty" jsonschema:"mistakes in the definition the platform repaired as it saved it"`
	Warnings   []string `json:"warnings,omitempty" jsonschema:"parts of the definition saved as written that will not work for visitors, although the checks pass; fix them before telling the user the feature works"`
	Checks     []Check  `json:"checks,omitempty" jsonschema:"result of rendering every GET route on preview: parameters filled from preview rows, signed-in routes as a visitor"`
	// OwnerActions are steps only the owner can take before http.fetch works.
	OwnerActions []string `json:"owner_actions,omitempty" jsonschema:"steps only the owner can take, signed in to Renoxium (allow hosts, enter API keys, turn on test calls for the preview). Tell the user all of them now, in this reply, each with its full link, before testing or saying the feature works"`
}

// PublishResult reports what Publish did.
type PublishResult struct {
	LiveVersion int      `json:"live_version"`
	LiveURL     string   `json:"live_url"`
	Migrations  []string `json:"migrations,omitempty" jsonschema:"schema changes applied to the live database"`
}

var (
	appNameRE = regexp.MustCompile(`^[a-z][a-z0-9-]{1,38}[a-z0-9]$`)
	reserved  = map[string]bool{}
	// ErrNotFound is returned for an unknown app or version.
	ErrNotFound = errors.New("not found")
)

// Names nobody may use for an app: platform words, and words that make a
// phishing page on <name>.renoxium.app look official. "customers" is the
// Cloudflare for SaaS fallback origin (customers.renoxium.app) that every
// custom domain's CNAME points to: an app with that name would receive them.
func init() {
	for _, n := range strings.Fields(`www api mcp admin app apps preview renoxium mail email smtp imap pop ftp ns ns1 ns2 dns cdn static assets
		files status help support docs blog billing pay payment payments invoice account accounts login signin signup auth oauth sso
		secure security verify verification password reset update wallet bank banking dashboard console root system official
		google gmail apple icloud microsoft outlook office paypal stripe facebook meta instagram whatsapp telegram amazon netflix
		chatgpt openai claude anthropic cloudflare github customers`) {
		reserved[n] = true
	}
}

// NameHold is how long a deleted app's name can only be taken again by the
// account that had it.
const NameHold = 90 * 24 * time.Hour

// Quotas per account and per app. There is no billing yet, so these keep
// one account from filling the server's disk.
const (
	MaxAppsPerOwner = 50
	keepVersions    = 50 // older versions are deleted, except the live one
	keepBackups     = 3  // publish backups kept per app
	MaxFilesPerApp  = 2000
)

// New opens (or creates) the platform under cfg.DataDir.
func New(cfg Config, sb sandbox.Sandbox) (*Platform, error) {
	if err := os.MkdirAll(filepath.Join(cfg.DataDir, "apps"), 0o755); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", "file:"+filepath.Join(cfg.DataDir, "platform.db")+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	for _, stmt := range []string{
		`CREATE TABLE IF NOT EXISTS apps (name TEXT PRIMARY KEY, created_at TEXT NOT NULL,
			live_version INTEGER NOT NULL DEFAULT 0, preview_version INTEGER NOT NULL DEFAULT 0)`,
		`CREATE TABLE IF NOT EXISTS versions (app TEXT NOT NULL, n INTEGER NOT NULL, definition TEXT NOT NULL,
			created_at TEXT NOT NULL, PRIMARY KEY (app, n))`,
		`CREATE TABLE IF NOT EXISTS domains (host TEXT PRIMARY KEY, app TEXT NOT NULL, stage TEXT NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS blobs (hash TEXT PRIMARY KEY, size INTEGER NOT NULL, content_type TEXT NOT NULL, created_at TEXT NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS app_files (app TEXT NOT NULL, hash TEXT NOT NULL REFERENCES blobs(hash), created_at TEXT NOT NULL,
			PRIMARY KEY (app, hash))`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			db.Close()
			return nil, err
		}
	}
	// Apps created before accounts existed have no owner and are reachable by nobody.
	if _, err := db.Exec(`ALTER TABLE apps ADD COLUMN owner TEXT NOT NULL DEFAULT ''`); err != nil && !strings.Contains(err.Error(), "duplicate column") {
		db.Close()
		return nil, err
	}
	// 1 once a stored photo has been made to fit (see FitStoredPhotos).
	if _, err := db.Exec(`ALTER TABLE blobs ADD COLUMN fitted INTEGER NOT NULL DEFAULT 0`); err != nil && !strings.Contains(err.Error(), "duplicate column") {
		db.Close()
		return nil, err
	}
	// Until this time preview makes real http.fetch calls (see fetch.go).
	if _, err := db.Exec(`ALTER TABLE apps ADD COLUMN preview_fetch_until INTEGER NOT NULL DEFAULT 0`); err != nil && !strings.Contains(err.Error(), "duplicate column") {
		db.Close()
		return nil, err
	}
	// How an app is paid for (see appplan.go).
	for _, col := range []string{`plan_kind TEXT NOT NULL DEFAULT ''`, `work_sub TEXT NOT NULL DEFAULT ''`,
		`work_status TEXT NOT NULL DEFAULT ''`, `work_period_end INTEGER NOT NULL DEFAULT 0`, `work_seats INTEGER NOT NULL DEFAULT 0`} {
		if _, err := db.Exec(`ALTER TABLE apps ADD COLUMN ` + col); err != nil && !strings.Contains(err.Error(), "duplicate column") {
			db.Close()
			return nil, err
		}
	}
	// A non-empty reason takes the app offline (see Suspend).
	if _, err := db.Exec(`ALTER TABLE apps ADD COLUMN suspended TEXT NOT NULL DEFAULT ''`); err != nil && !strings.Contains(err.Error(), "duplicate column") {
		db.Close()
		return nil, err
	}
	store, err := files.Open(filepath.Join(cfg.DataDir, "files"))
	if err != nil {
		db.Close()
		return nil, err
	}
	if cfg.ScriptsPerApp <= 0 {
		cfg.ScriptsPerApp = 2
	}
	for _, f := range []struct {
		v *int
		d int
	}{{&cfg.Daily.EmailsPerApp, DefaultDaily.EmailsPerApp}, {&cfg.Daily.Emails, DefaultDaily.Emails},
		{&cfg.Daily.WhatsAppPerApp, DefaultDaily.WhatsAppPerApp}, {&cfg.Daily.WhatsApp, DefaultDaily.WhatsApp}} {
		if *f.v <= 0 {
			*f.v = f.d
		}
	}
	if cfg.ScriptsPerOwner <= 0 {
		cfg.ScriptsPerOwner = cfg.ScriptsPerApp
	}
	pl := &Platform{cfg: cfg, db: db, sb: sb, files: store,
		stages: map[string]*stage{}, logs: map[string]*applog.Log{}, intake: map[string]*window{}, slots: map[string]chan struct{}{}, limits: map[string]*window{}, rids: map[string]*ridEntry{}, shots: make(chan struct{}, shotsAtOnce)}
	if err := pl.ensureDomainTable(); err != nil {
		db.Close()
		return nil, err
	}
	if err := pl.ensureStaffTable(); err != nil {
		db.Close()
		return nil, err
	}
	if err := pl.ensureAbuseTable(); err != nil {
		db.Close()
		return nil, err
	}
	if err := pl.ensureUsageTable(); err != nil {
		db.Close()
		return nil, err
	}
	if err := pl.ensureAlertTable(); err != nil {
		db.Close()
		return nil, err
	}
	pl.live.init(cfg.PreviewKey)
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS released_names (name TEXT PRIMARY KEY, owner TEXT NOT NULL, released_at INTEGER NOT NULL)`); err != nil {
		db.Close()
		return nil, err
	}
	if _, err := db.Exec(renameSchema); err != nil {
		return nil, err
	}
	if err := pl.ensureLimitTable(); err != nil {
		db.Close()
		return nil, err
	}
	if err := pl.ensureFetchTables(); err != nil {
		db.Close()
		return nil, err
	}
	if err := pl.ensureJobTable(); err != nil {
		db.Close()
		return nil, err
	}
	if err := pl.ensurePaymentTables(); err != nil {
		db.Close()
		return nil, err
	}
	if err := pl.ensurePrivateFileTable(); err != nil {
		db.Close()
		return nil, err
	}
	if err := pl.ensureTrafficTables(); err != nil {
		db.Close()
		return nil, err
	}
	if err := pl.ensureGrowthTables(); err != nil {
		db.Close()
		return nil, err
	}
	if err := pl.ensureMeterTables(); err != nil {
		db.Close()
		return nil, err
	}
	return pl, nil
}

// Close releases every open database.
func (p *Platform) Close() {
	p.bg.Wait()
	p.flushTraffic()
	p.flushMeter(true)
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, st := range p.stages {
		st.db.Close()
		st.admin.Close()
	}
	for _, l := range p.logs {
		l.Close()
	}
	p.db.Close()
}

func now() string { return time.Now().UTC().Format(time.RFC3339) }

func (p *Platform) appDir(app string) string { return filepath.Join(p.cfg.DataDir, "apps", app) }

func (p *Platform) dbPath(app, stageName string) string {
	return filepath.Join(p.appDir(app), stageName+".db")
}

func (p *Platform) host(app, stageName string) string {
	if stageName == Preview && !p.IsDemo(app) { // a demo's preview has the plain address
		return app + "--preview." + p.cfg.AppsDomain
	}
	return app + "." + p.cfg.AppsDomain
}

// previewURL is where people open a preview: the owner's dashboard link
// when previews are private, else the preview host itself.
func (p *Platform) previewURL(app string) string {
	if len(p.cfg.PreviewKey) > 0 && !p.IsDemo(app) {
		return p.cfg.SiteURL + "/apps/" + app + "/preview"
	}
	return p.URL(app, Preview)
}

// URL is the public address of an app stage.
func (p *Platform) URL(app, stageName string) string {
	if stageName == Live {
		if h, ok := p.liveHosts.Load(app); ok {
			return "https://" + h.(string) // an active custom domain is the main address
		}
	}
	return p.cfg.PublicScheme + "://" + p.host(app, stageName) + p.cfg.PublicPort
}

// CreateApp registers a new, empty app owned by owner, and its two hosts.
func (p *Platform) CreateApp(owner, name string) (AppInfo, error) {
	if owner == "" {
		return AppInfo{}, errors.New("an app needs an owner")
	}
	if !appNameRE.MatchString(name) || strings.Contains(name, "--") {
		return AppInfo{}, fmt.Errorf("app name %q is not allowed: use 3-40 characters, lowercase letters, digits and single hyphens, starting with a letter", name)
	}
	if reserved[name] || strings.HasPrefix(name, DemoPrefix) {
		return AppInfo{}, fmt.Errorf("app name %q is reserved; choose a name that includes the business name", name)
	}
	var count int
	if err := p.db.QueryRow(`SELECT count(*) FROM apps WHERE owner = ?`, owner).Scan(&count); err != nil {
		return AppInfo{}, err
	}
	if err := p.appLimitError(owner, count); err != nil {
		return AppInfo{}, err
	}
	if err := os.MkdirAll(p.appDir(name), 0o755); err != nil {
		return AppInfo{}, err
	}
	tx, err := p.db.Begin()
	if err != nil {
		return AppInfo{}, err
	}
	defer tx.Rollback()
	// A deleted app's name stays with its last owner for a while: its
	// address is in customers' bookmarks, search results and old emails, and
	// must not start serving someone else's pages overnight.
	// So do the old addresses of a renamed app while they redirect.
	if held := p.heldBy(tx, name); held != "" && held != owner {
		return AppInfo{}, fmt.Errorf("app name %q is already taken", name)
	}
	// Its owner taking the name back ends the redirects from it.
	tx.Exec(`DELETE FROM host_redirects WHERE host IN (?, ?, ?, ?)`, p.host(name, Live), p.host(name, Preview), p.idHost(name, Live), p.idHost(name, Preview))
	if _, err := tx.Exec(`INSERT INTO apps (name, created_at, owner) VALUES (?, ?, ?)`, name, now(), owner); err != nil {
		var mine int
		tx.QueryRow(`SELECT count(*) FROM apps WHERE name = ? AND owner = ?`, name, owner).Scan(&mine)
		if mine > 0 {
			return AppInfo{}, fmt.Errorf("you already have an app called %q: ask the user whether to change that app (edit_app) or create a new one under another name", name)
		}
		return AppInfo{}, fmt.Errorf("app name %q is already taken", name)
	}
	for _, s := range []string{Live, Preview} {
		if _, err := tx.Exec(`INSERT INTO domains (host, app, stage) VALUES (?, ?, ?)`, p.host(name, s), name, s); err != nil {
			return AppInfo{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return AppInfo{}, err
	}
	p.funnel(owner, FunnelApp, name, "")
	return p.Info(name)
}

// CheckOwner returns ErrNotFound unless user owns app, so apps of other
// users are indistinguishable from apps that do not exist.
func (p *Platform) CheckOwner(user, app string) error {
	var owner string
	err := p.db.QueryRow(`SELECT owner FROM apps WHERE name = ?`, app).Scan(&owner)
	if err != nil || owner == "" || owner != user {
		return fmt.Errorf("app %q: %w", app, ErrNotFound)
	}
	return nil
}

// Apps lists the apps owned by user.
func (p *Platform) Apps(user string) ([]AppInfo, error) {
	rows, err := p.db.Query(`SELECT name FROM apps WHERE owner = ? AND owner != '' ORDER BY name`, user)
	if err != nil {
		return nil, err
	}
	var names []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			rows.Close()
			return nil, err
		}
		names = append(names, n)
	}
	rows.Close()
	out := []AppInfo{}
	for _, n := range names {
		info, err := p.Info(n)
		if err != nil {
			return nil, err
		}
		out = append(out, info)
	}
	return out, nil
}

// Info returns the current state of an app.
func (p *Platform) Info(name string) (AppInfo, error) {
	info := AppInfo{App: name, LiveURL: p.URL(name, Live), PreviewURL: p.previewURL(name)}
	err := p.db.QueryRow(`SELECT live_version, preview_version FROM apps WHERE name = ?`, name).
		Scan(&info.LiveVersion, &info.PreviewVersion)
	if errors.Is(err, sql.ErrNoRows) {
		return info, fmt.Errorf("app %q: %w", name, ErrNotFound)
	}
	return info, err
}

// Definition returns a stored version of an app; version 0 means the newest.
func (p *Platform) Definition(name string, version int) (*appdef.App, int, error) {
	var raw string
	var n int
	var err error
	if version == 0 {
		err = p.db.QueryRow(`SELECT n, definition FROM versions WHERE app = ? ORDER BY n DESC LIMIT 1`, name).Scan(&n, &raw)
	} else {
		err = p.db.QueryRow(`SELECT n, definition FROM versions WHERE app = ? AND n = ?`, name, version).Scan(&n, &raw)
	}
	if errors.Is(err, sql.ErrNoRows) {
		return nil, 0, fmt.Errorf("app %q version %d: %w", name, version, ErrNotFound)
	}
	if err != nil {
		return nil, 0, err
	}
	var def appdef.App
	if err := json.Unmarshal([]byte(raw), &def); err != nil {
		return nil, 0, err
	}
	return &def, n, nil
}

// ApplyChange validates a definition, stores it as a new version and rebuilds
// the preview stage from a snapshot of live data. Live is not touched.
func (p *Platform) ApplyChange(ctx context.Context, name string, def *appdef.App) (ApplyResult, error) {
	if _, err := p.Info(name); err != nil {
		return ApplyResult{}, err
	}
	fixed, styleProblems := def.FixStyleSlots()
	problems := append(append(def.Validate(), styleProblems...), p.checkScripts(def)...)
	raw, err := json.Marshal(def)
	if err != nil {
		return ApplyResult{}, err
	}
	if len(raw) > appdef.MaxSize {
		problems = append(problems, fmt.Sprintf("definition is %d bytes; the limit is %d", len(raw), appdef.MaxSize))
	}
	if len(problems) > 0 {
		return ApplyResult{Problems: problems}, nil
	}

	p.change.Lock()
	defer p.change.Unlock()

	// Build the preview database first; a failed migration leaves no new version.
	migrations, problem, err := p.rebuildPreview(name, def)
	if err != nil {
		return ApplyResult{}, err
	}
	if problem != "" {
		return ApplyResult{Problems: []string{problem}}, nil
	}

	var n int
	tx, err := p.db.Begin()
	if err != nil {
		return ApplyResult{}, err
	}
	defer tx.Rollback()
	if err := tx.QueryRow(`SELECT coalesce(max(n), 0) + 1 FROM versions WHERE app = ?`, name).Scan(&n); err != nil {
		return ApplyResult{}, err
	}
	if _, err := tx.Exec(`INSERT INTO versions (app, n, definition, created_at) VALUES (?, ?, ?, ?)`, name, n, string(raw), now()); err != nil {
		return ApplyResult{}, err
	}
	if _, err := tx.Exec(`UPDATE apps SET preview_version = ? WHERE name = ?`, n, name); err != nil {
		return ApplyResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return ApplyResult{}, err
	}

	p.db.Exec(`DELETE FROM versions WHERE app = ? AND n <= ? AND n != (SELECT live_version FROM apps WHERE name = ?)`, name, n-keepVersions, name)
	res := ApplyResult{OK: true, Version: n, PreviewURL: p.previewURL(name), Migrations: migrations, Fixed: fixed,
		Warnings: p.browserFetchWarnings(name, def)}
	st, err := p.loadStage(name, Preview)
	if err != nil {
		return res, err
	}
	res.Checks = p.checkRoutes(ctx, st, def)
	for _, problem := range checkSitemap(ctx, st) {
		res.Checks = append(res.Checks, Check{Route: "GET " + SitemapPath, Status: http.StatusInternalServerError, Error: problem})
	}
	if hosts, secrets, err := p.Connections(name); err == nil {
		link := p.cfg.SiteURL + "/apps/" + name + "/connections"
		for _, h := range hosts {
			if !h.Allowed {
				res.OwnerActions = append(res.OwnerActions, "allow "+h.Host+" at "+link)
			}
		}
		for _, s := range secrets {
			if !s.Set || s.Mismatch {
				res.OwnerActions = append(res.OwnerActions, "enter "+s.Name+" at "+link)
			}
		}
		// The preview makes no real calls until the owner turns them on, as
		// a second step after allowing: say both now, not one failure at a time.
		if len(hosts) > 0 && !p.PreviewFetchOn(name) {
			res.OwnerActions = append(res.OwnerActions, "to test these calls on the preview, turn on test calls for 2 hours at "+link+
				" (after allowing the hosts); until then http.fetch fails on the preview")
		}
	}
	return res, nil
}

// rebuildPreview replaces an app's preview data with a copy of live data
// (or an empty database before the first publish) migrated to def, seed rows
// included. problem explains a failed migration. The caller holds p.change.
func (p *Platform) rebuildPreview(name string, def *appdef.App) (migrations []string, problem string, err error) {
	p.dropStage(name, Preview)
	p.dropTestSessions(name)
	// Preview uploads belong to the throwaway preview data; they are never
	// copied to or from live.
	if err := p.dropPrivateFiles(name, Preview); err != nil {
		return nil, "", err
	}
	prev := p.dbPath(name, Preview)
	for _, suffix := range []string{"", "-wal", "-shm"} {
		os.Remove(prev + suffix)
	}
	if _, err := os.Stat(p.dbPath(name, Live)); err == nil {
		if err := tenantdb.Snapshot(p.dbPath(name, Live), prev); err != nil {
			return nil, "", fmt.Errorf("copy live data to preview: %w", err)
		}
	}
	migrations, err = tenantdb.Migrate(prev, def.Tables, def.Seed)
	if err != nil {
		return nil, "database migration failed on preview: " + err.Error(), nil
	}
	return migrations, "", nil
}

// Publish makes a stored version live. version 0 means the current preview
// version. The live database is backed up, then migrated additively.
func (p *Platform) Publish(name string, version int) (PublishResult, error) {
	info, err := p.Info(name)
	if err != nil {
		return PublishResult{}, err
	}
	if version == 0 {
		version = info.PreviewVersion
	}
	if version == 0 {
		return PublishResult{}, errors.New("nothing to publish: call apply_change first")
	}
	def, _, err := p.Definition(name, version)
	if err != nil {
		return PublishResult{}, err
	}
	if err := p.ownerVerified(name); err != nil {
		return PublishResult{}, err
	}
	if p.isSuspended(name) {
		return PublishResult{}, errors.New("this app is suspended")
	}
	if pl := p.planFor(name, def); !pl.Ready {
		return PublishResult{}, errors.New(pl.Problem)
	}

	p.change.Lock()
	defer p.change.Unlock()

	live := p.dbPath(name, Live)
	if _, err := os.Stat(live); err == nil {
		backup := filepath.Join(p.appDir(name), fmt.Sprintf("backup-before-v%d.db", version))
		if err := tenantdb.Snapshot(live, backup); err != nil {
			return PublishResult{}, fmt.Errorf("backup live data: %w", err)
		}
		p.pruneBackups(name)
	}
	migrations, err := tenantdb.Migrate(live, def.Tables, def.Seed)
	if err != nil {
		return PublishResult{}, fmt.Errorf("database migration failed; live was not changed: %w", err)
	}
	if _, err := p.db.Exec(`UPDATE apps SET live_version = ? WHERE name = ?`, version, name); err != nil {
		return PublishResult{}, err
	}
	p.dropStage(name, Live)
	p.funnel(p.ownerOf(name), FunnelPublish, name, guessTemplate(def))
	return PublishResult{LiveVersion: version, LiveURL: p.URL(name, Live), Migrations: migrations}, nil
}

// Logs returns events for an app.
func (p *Platform) Logs(name string, f applog.Filter) ([]applog.Event, error) {
	if _, err := p.Info(name); err != nil {
		return nil, err
	}
	l, err := p.log(name)
	if err != nil {
		return nil, err
	}
	return l.Query(f)
}

func (p *Platform) log(app string) (*applog.Log, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if l := p.logs[app]; l != nil {
		return l, nil
	}
	l, err := applog.Open(filepath.Join(p.appDir(app), "logs.db"))
	if err != nil {
		return nil, err
	}
	p.logs[app] = l
	return l, nil
}

func (p *Platform) event(st *stage, source, level, rid, msg string) {
	l, err := p.log(st.app)
	if err != nil {
		return
	}
	l.Add(applog.Event{Stage: st.name, Version: st.version, Source: source, Level: level, RequestID: rid, Message: msg})
	if level == "error" && st.name == Live && source != "browser" {
		p.errors.note(st.app, msg)
	}
}

// Resolve finds the app and stage served on a request host. Hosts are looked
// up in the domains table, so custom domains only need a row there.
func (p *Platform) Resolve(hostport string) (app, stageName string, ok bool) {
	host := strings.ToLower(hostport)
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	if e, hit := p.hosts.Load(host); hit {
		he := e.(hostEntry)
		return he.app, he.stage, true
	}
	var he hostEntry
	if err := p.db.QueryRow(`SELECT app, stage FROM domains WHERE host = ?`, host).Scan(&he.app, &he.stage); err != nil {
		return "", "", false
	}
	p.hosts.Store(host, he)
	return he.app, he.stage, true
}

// loadStage returns the running stage of an app, or nil if that stage has no
// version yet.
func (p *Platform) loadStage(app, stageName string) (*stage, error) {
	key := app + "/" + stageName
	p.mu.Lock()
	defer p.mu.Unlock()
	if st := p.stages[key]; st != nil {
		return st, nil
	}
	info, err := p.Info(app)
	if err != nil {
		return nil, err
	}
	version := info.LiveVersion
	if stageName == Preview {
		version = info.PreviewVersion
	}
	if version == 0 {
		return nil, nil
	}
	def, _, err := p.Definition(app, version)
	if err != nil {
		return nil, err
	}
	tmpl, err := def.ParseTemplates()
	if err != nil {
		return nil, err
	}
	if err := tenantdb.EnsurePlatformTables(p.dbPath(app, stageName)); err != nil {
		return nil, err
	}
	admin, err := tenantdb.OpenAdmin(p.dbPath(app, stageName))
	if err != nil {
		return nil, err
	}
	db, err := tenantdb.Open(p.dbPath(app, stageName))
	if err != nil {
		admin.Close()
		return nil, err
	}
	st := &stage{app: app, name: stageName, version: version, def: def, tmpl: tmpl, db: db, admin: admin}
	p.stages[key] = st
	return st, nil
}

func (p *Platform) dropStage(app, stageName string) {
	key := app + "/" + stageName
	p.mu.Lock()
	defer p.mu.Unlock()
	if st := p.stages[key]; st != nil {
		st.db.Close()
		st.admin.Close()
		delete(p.stages, key)
	}
}

// pruneBackups keeps the newest keepBackups publish backups of an app.
func (p *Platform) pruneBackups(app string) {
	paths, _ := filepath.Glob(filepath.Join(p.appDir(app), "backup-before-v*.db"))
	type backup struct {
		path string
		mod  time.Time
	}
	var list []backup
	for _, path := range paths {
		if fi, err := os.Stat(path); err == nil {
			list = append(list, backup{path, fi.ModTime()})
		}
	}
	sort.Slice(list, func(i, j int) bool { return list[i].mod.After(list[j].mod) })
	for i := keepBackups; i < len(list); i++ {
		os.Remove(list[i].path)
	}
}

// Note writes an informational line to an app's live log.
func (p *Platform) Note(app, msg string) {
	if l, err := p.log(app); err == nil {
		l.Add(applog.Event{Stage: Live, Source: "admin", Level: "info", Message: msg})
	}
}
