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
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"

	"github.com/arif-rachim/renoxium/internal/appdef"
	"github.com/arif-rachim/renoxium/internal/applog"
	"github.com/arif-rachim/renoxium/internal/sandbox"
	"github.com/arif-rachim/renoxium/internal/tenantdb"
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
}

// Platform is safe for concurrent use.
type Platform struct {
	cfg Config
	db  *sql.DB
	sb  sandbox.Sandbox

	change sync.Mutex // serialises ApplyChange and Publish

	mu     sync.Mutex
	stages map[string]*stage
	logs   map[string]*applog.Log
	hosts  sync.Map // host -> hostEntry
	intake map[string]*window
}

type stage struct {
	app     string
	name    string
	version int
	def     *appdef.App
	tmpl    *template.Template
	db      *tenantdb.DB
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
	reserved  = map[string]bool{"www": true, "api": true, "mcp": true, "admin": true, "app": true, "preview": true, "renoxium": true, "mail": true}
	// ErrNotFound is returned for an unknown app or version.
	ErrNotFound = errors.New("not found")
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
	return &Platform{cfg: cfg, db: db, sb: sb,
		stages: map[string]*stage{}, logs: map[string]*applog.Log{}, intake: map[string]*window{}}, nil
}

// Close releases every open database.
func (p *Platform) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, st := range p.stages {
		st.db.Close()
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

// URL is the public address of an app stage.
func (p *Platform) URL(app, stageName string) string {
	return p.cfg.PublicScheme + "://" + p.host(app, stageName) + p.cfg.PublicPort
}

// CreateApp registers a new, empty app owned by owner, and its two hosts.
func (p *Platform) CreateApp(owner, name string) (AppInfo, error) {
	if owner == "" {
		return AppInfo{}, errors.New("an app needs an owner")
	}
	if !appNameRE.MatchString(name) || strings.Contains(name, "--") || reserved[name] {
		return AppInfo{}, fmt.Errorf("app name %q is not allowed: use 3-40 characters, lowercase letters, digits and single hyphens, starting with a letter", name)
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
	info := AppInfo{App: name, LiveURL: p.URL(name, Live), PreviewURL: p.URL(name, Preview)}
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

	res := ApplyResult{OK: true, Version: n, PreviewURL: p.URL(name, Preview), Migrations: migrations}
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

	p.change.Lock()
	defer p.change.Unlock()

	live := p.dbPath(name, Live)
	if _, err := os.Stat(live); err == nil {
		backup := filepath.Join(p.appDir(name), fmt.Sprintf("backup-before-v%d.db", version))
		if err := tenantdb.Snapshot(live, backup); err != nil {
			return PublishResult{}, fmt.Errorf("backup live data: %w", err)
		}
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
	db, err := tenantdb.Open(p.dbPath(app, stageName))
	if err != nil {
		return nil, err
	}
	st := &stage{app: app, name: stageName, version: version, def: def, tmpl: tmpl, db: db}
	p.stages[key] = st
	return st, nil
}

func (p *Platform) dropStage(app, stageName string) {
	key := app + "/" + stageName
	p.mu.Lock()
	defer p.mu.Unlock()
	if st := p.stages[key]; st != nil {
		st.db.Close()
		delete(p.stages, key)
	}
}
