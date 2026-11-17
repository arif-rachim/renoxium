// Package platform is the Renoxium engine: it stores apps and their versions,
// builds the preview and live stages, and knows which app a host belongs to.
package platform

import (
	"context"
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
	// Stripe takes payments for live apps on their owners' Stripe
	// accounts. Nil turns live checkout off; previews always simulate it.
	Stripe *stripe.Client
	// WhatsApp sends sign-in codes from live. Nil turns phone sign-in off
	// on live; preview always offers it, with codes in the app log.
	WhatsApp whatsapp.Sender
	// WhatsAppNotices sends notify.owner notices to owners who confirmed a
	// number. Nil turns that off.
	WhatsAppNotices whatsapp.Notifier
	// OwnerPlan returns an account's plan: "free", "pro" or "business".
	// Free-plan apps pay a cut of card payments, show a small badge and
	// are limited in number. Nil means no plans: nothing is limited.
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
	hosts  sync.Map // host -> hostEntry
	owners sync.Map // app -> owner
	// liveHosts maps an app to its active custom domain.
	liveHosts sync.Map
	intake    map[string]*window
	slots     map[string]chan struct{} // per-app script concurrency
	limits    map[string]*window       // visitor sign-in rate limits
	rids      map[string]*ridEntry     // request IDs of served pages

	suspendedAt time.Time       // when suspended was last read
	suspended   map[string]bool // apps taken offline
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
	PreviewURL     string `json:"preview_url"`
}

// Check is the result of rendering one route during ApplyChange.
type Check struct {
	Route  string `json:"route"`
	Status int    `json:"status"`
	Error  string `json:"error,omitempty"`
}

// ApplyResult reports what ApplyChange did.
type ApplyResult struct {
	OK         bool     `json:"ok" jsonschema:"false when the definition was rejected; see problems"`
	Problems   []string `json:"problems,omitempty" jsonschema:"why the definition was rejected; fix these and call apply_change again"`
	Version    int      `json:"version,omitempty"`
	PreviewURL string   `json:"preview_url,omitempty"`
	Migrations []string `json:"migrations,omitempty" jsonschema:"schema changes applied to the preview database"`
	Checks     []Check  `json:"checks,omitempty" jsonschema:"result of rendering every GET route without path parameters on preview"`
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
// phishing page on <name>.renoxium.app look official.
func init() {
	for _, n := range strings.Fields(`www api mcp admin app apps preview renoxium mail email smtp imap pop ftp ns ns1 ns2 dns cdn static assets
		files status help support docs blog billing pay payment payments invoice account accounts login signin signup auth oauth sso
		secure security verify verification password reset update wallet bank banking dashboard console root system official
		google gmail apple icloud microsoft outlook office paypal stripe facebook meta instagram whatsapp telegram amazon netflix
		chatgpt openai claude anthropic cloudflare github`) {
		reserved[n] = true
	}
}

// Quotas per account and per app. There is no billing yet, so these keep
// one account from filling the server's disk.
const (
	MaxAppsPerOwner = 10
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
		stages: map[string]*stage{}, logs: map[string]*applog.Log{}, intake: map[string]*window{}, slots: map[string]chan struct{}{}, limits: map[string]*window{}, rids: map[string]*ridEntry{}}
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
	if err := pl.ensurePaymentTables(); err != nil {
		db.Close()
		return nil, err
	}
	return pl, nil
}

// Close releases every open database.
func (p *Platform) Close() {
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
	if stageName == Preview {
		return app + "--preview." + p.cfg.AppsDomain
	}
	return app + "." + p.cfg.AppsDomain
}

// previewURL is where people open a preview: the owner's dashboard link
// when previews are private, else the preview host itself.
func (p *Platform) previewURL(app string) string {
	if len(p.cfg.PreviewKey) > 0 {
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
	if reserved[name] {
		return AppInfo{}, fmt.Errorf("app name %q is reserved; choose a name that includes the business name", name)
	}
	var count int
	if err := p.db.QueryRow(`SELECT count(*) FROM apps WHERE owner = ?`, owner).Scan(&count); err != nil {
		return AppInfo{}, err
	}
	if count >= MaxAppsPerOwner {
		return AppInfo{}, fmt.Errorf("an account can have at most %d apps", MaxAppsPerOwner)
	}
	if p.cfg.OwnerPlan != nil {
		plan := p.planOf(owner)
		if limit := AppsPerPlan[plan]; count >= limit {
			return AppInfo{}, fmt.Errorf("the %s plan allows %d app(s) and this account has %d; upgrade the plan at %s/billing to create more",
				plan, limit, count, p.cfg.SiteURL)
		}
	}
	if err := os.MkdirAll(p.appDir(name), 0o755); err != nil {
		return AppInfo{}, err
	}
	tx, err := p.db.Begin()
	if err != nil {
		return AppInfo{}, err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`INSERT INTO apps (name, created_at, owner) VALUES (?, ?, ?)`, name, now(), owner); err != nil {
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
	problems := def.Validate()
	for sname, src := range def.Scripts {
		if err := p.sb.Check(src); err != nil {
			problems = append(problems, fmt.Sprintf("script %q: %v", sname, err))
		}
	}
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
	p.dropStage(name, Preview)
	prev := p.dbPath(name, Preview)
	for _, suffix := range []string{"", "-wal", "-shm"} {
		os.Remove(prev + suffix)
	}
	if _, err := os.Stat(p.dbPath(name, Live)); err == nil {
		if err := tenantdb.Snapshot(p.dbPath(name, Live), prev); err != nil {
			return ApplyResult{}, fmt.Errorf("copy live data to preview: %w", err)
		}
	}
	migrations, err := tenantdb.Migrate(prev, def.Tables, def.Seed)
	if err != nil {
		return ApplyResult{Problems: []string{"database migration failed on preview: " + err.Error()}}, nil
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
	res := ApplyResult{OK: true, Version: n, PreviewURL: p.previewURL(name), Migrations: migrations}
	st, err := p.loadStage(name, Preview)
	if err != nil {
		return res, err
	}
	for _, r := range def.Routes {
		if r.Method != http.MethodGet || r.HasParams() {
			continue
		}
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, r.Path, nil)
		out := p.handle(st, req, newRequestID())
		res.Checks = append(res.Checks, Check{Route: "GET " + r.Path, Status: out.status, Error: out.errMsg})
	}
	return res, nil
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
