package platform

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"time"
)

// Renaming an app changes its addresses: <name>.<apps domain> and the
// preview and sign-in hosts. The old addresses keep answering with a
// permanent redirect to the same path on the new ones for a year (links in
// emails, bookmarks, search results), and nobody else can take the old
// name while they do. Stripe keeps the old name in the metadata of
// payments and subscriptions it already holds, so webhooks resolve it
// through app_renames for good.
//
// A rename is only ever done for the owner: the agent's rename_app tool
// returns a link, and the owner confirms it signed in.

const (
	// RenameRedirect is how long old addresses redirect after a rename.
	RenameRedirect = 365 * 24 * time.Hour
	// RenameEvery is the least time between two renames of an app.
	RenameEvery = 30 * 24 * time.Hour
)

const renameSchema = `CREATE TABLE IF NOT EXISTS app_renames (old TEXT PRIMARY KEY, new TEXT NOT NULL, owner TEXT NOT NULL, renamed_at INTEGER NOT NULL);
CREATE INDEX IF NOT EXISTS app_renames_new ON app_renames (new);
CREATE TABLE IF NOT EXISTS host_redirects (host TEXT PRIMARY KEY, target TEXT NOT NULL, owner TEXT NOT NULL, until INTEGER NOT NULL)`

// CheckNewName says whether app may be renamed to name now, without
// changing anything: the same rules as a new app's name, the name free (or
// held for this owner), and no rename in the last 30 days.
func (p *Platform) CheckNewName(app, name string) error {
	if _, err := p.Info(app); err != nil {
		return err
	}
	owner := p.ownerOf(app)
	if name == app {
		return errors.New("that is already the app's name")
	}
	if p.IsDemo(app) {
		return errors.New("template demos cannot be renamed")
	}
	if err := p.checkName(name); err != nil {
		return err
	}
	var exists int
	if p.db.QueryRow(`SELECT 1 FROM apps WHERE name = ?`, name).Scan(&exists) == nil {
		return fmt.Errorf("app name %q is already taken", name)
	}
	if held := p.heldBy(p.db, name); held != "" && held != owner {
		return fmt.Errorf("app name %q is already taken", name)
	}
	var last int64
	if p.db.QueryRow(`SELECT max(renamed_at) FROM app_renames WHERE new = ?`, app).Scan(&last) == nil && last > 0 {
		if next := time.Unix(last, 0).Add(RenameEvery); time.Now().Before(next) {
			return fmt.Errorf("this app was renamed recently; it can be renamed again from %s", next.UTC().Format("2 Jan 2006"))
		}
	}
	return nil
}

// checkName applies the rules every app name follows.
func (p *Platform) checkName(name string) error {
	if !appNameRE.MatchString(name) || strings.Contains(name, "--") {
		return fmt.Errorf("app name %q is not allowed: use 3-40 characters, lowercase letters, digits and single hyphens, starting with a letter", name)
	}
	if reserved[name] || strings.HasPrefix(name, DemoPrefix) {
		return fmt.Errorf("app name %q is reserved; choose a name that includes the business name", name)
	}
	return nil
}

// heldBy returns the account a free name is kept for: the owner of a
// recently deleted app, or of an app renamed away from it whose old
// addresses still redirect. "" means anyone may take it.
func (p *Platform) heldBy(q rowQuerier, name string) string {
	var owner string
	if q.QueryRow(`SELECT owner FROM released_names WHERE name = ? AND released_at > ?`, name, time.Now().Add(-NameHold).Unix()).Scan(&owner) == nil {
		return owner
	}
	if q.QueryRow(`SELECT owner FROM host_redirects WHERE host = ? AND until > ?`, p.host(name, Live), time.Now().Unix()).Scan(&owner) == nil {
		return owner
	}
	return ""
}

// RenameApp moves an app to a new name: its data directory, every
// platform record that names it, and its hosts. The old hosts redirect.
func (p *Platform) RenameApp(ctx context.Context, app, name string) (AppInfo, error) {
	if err := p.CheckNewName(app, name); err != nil {
		return AppInfo{}, err
	}
	owner := p.ownerOf(app)
	// Counters kept in memory are written under the old name first, so the
	// rows move with the rest.
	p.flushTraffic()
	p.flushMeter(true)

	p.change.Lock()
	defer p.change.Unlock()
	if err := p.CheckNewName(app, name); err != nil { // again, under the lock
		return AppInfo{}, err
	}
	var hosts []string
	if rows, err := p.db.Query(`SELECT host FROM domains WHERE app = ?`, app); err == nil {
		for rows.Next() {
			var h string
			rows.Scan(&h)
			hosts = append(hosts, h)
		}
		rows.Close()
	}
	tables, err := p.tablesWithAppColumn()
	if err != nil {
		return AppInfo{}, err
	}

	// Close what holds the old directory open, then move it.
	p.dropStage(app, Live)
	p.dropStage(app, Preview)
	p.mu.Lock()
	if l := p.logs[app]; l != nil {
		l.Close()
		delete(p.logs, app)
	}
	p.mu.Unlock()
	if _, err := os.Stat(p.appDir(name)); err == nil {
		// Left over from a deleted app of the same owner (its name was held for them).
		if err := os.RemoveAll(p.appDir(name)); err != nil {
			return AppInfo{}, err
		}
	}
	if err := os.Rename(p.appDir(app), p.appDir(name)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return AppInfo{}, fmt.Errorf("move app data: %w", err)
	}
	undo := func() { os.Rename(p.appDir(name), p.appDir(app)) }

	tx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		undo()
		return AppInfo{}, err
	}
	defer tx.Rollback()
	steps := []struct {
		sql  string
		args []any
	}{
		{`UPDATE apps SET name = ? WHERE name = ?`, []any{name, app}},
		{`DELETE FROM released_names WHERE name = ?`, []any{name}},
		{`DELETE FROM host_redirects WHERE target IN (?, ?) OR host IN (?, ?, ?, ?)`, []any{p.host(name, Live), p.host(name, Preview),
			p.host(name, Live), p.host(name, Preview), p.idHost(name, Live), p.idHost(name, Preview)}},
		// Earlier renames now lead to the new name.
		{`UPDATE app_renames SET new = ? WHERE new = ?`, []any{name, app}},
		{`INSERT OR REPLACE INTO app_renames (old, new, owner, renamed_at) VALUES (?, ?, ?, ?)`, []any{app, name, owner, time.Now().Unix()}},
	}
	for _, t := range tables {
		steps = append(steps, struct {
			sql  string
			args []any
		}{`UPDATE OR IGNORE "` + t + `" SET app = ? WHERE app = ?`, []any{name, app}})
	}
	until := time.Now().Add(RenameRedirect).Unix()
	for _, stg := range []string{Live, Preview} {
		steps = append(steps,
			struct {
				sql  string
				args []any
			}{`UPDATE domains SET host = ? WHERE host = ?`, []any{p.host(name, stg), p.host(app, stg)}},
			struct {
				sql  string
				args []any
			}{`INSERT OR REPLACE INTO host_redirects (host, target, owner, until) VALUES (?, ?, ?, ?)`, []any{bareHost(p.host(app, stg)), p.host(name, stg), owner, until}},
			struct {
				sql  string
				args []any
			}{`INSERT OR REPLACE INTO host_redirects (host, target, owner, until) VALUES (?, ?, ?, ?)`, []any{bareHost(p.idHost(app, stg)), p.idHost(name, stg), owner, until}},
		)
	}
	// Redirects that pointed at the old name now point at the new one.
	steps = append(steps, struct {
		sql  string
		args []any
	}{`UPDATE host_redirects SET target = replace(target, ?, ?) WHERE target IN (?, ?, ?, ?)`, []any{app, name,
		p.host(app, Live), p.host(app, Preview), p.idHost(app, Live), p.idHost(app, Preview)}})
	for _, s := range steps {
		if _, err := tx.ExecContext(ctx, s.sql, s.args...); err != nil {
			undo()
			return AppInfo{}, fmt.Errorf("rename: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		undo()
		return AppInfo{}, err
	}

	for _, h := range hosts {
		p.hosts.Delete(h)
	}
	p.owners.Delete(app)
	p.forgetLive(app)
	p.dropTestSessions(app)
	if h, ok := p.liveHosts.LoadAndDelete(app); ok {
		p.liveHosts.Store(name, h) // a custom domain stays the app's main address
	}
	p.loadThrottles()
	return p.Info(name)
}

// tablesWithAppColumn lists the platform tables that name an app in a
// column called app, so a rename reaches tables added later too.
func (p *Platform) tablesWithAppColumn() ([]string, error) {
	rows, err := p.db.Query(`SELECT m.name FROM sqlite_master m, pragma_table_info(m.name) c
		WHERE m.type = 'table' AND c.name = 'app' AND m.name NOT IN ('app_renames')`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// renamedHost finds the new host for an old one, if it still redirects.
func (p *Platform) renamedHost(hostport string) (string, bool) {
	host := bareHost(strings.ToLower(hostport))
	var target string
	if p.db.QueryRow(`SELECT target FROM host_redirects WHERE host = ? AND until > ?`, host, time.Now().Unix()).Scan(&target) != nil {
		return "", false
	}
	return target, true
}

// currentName follows renames from a name an app once had (Stripe keeps it
// in the metadata of earlier payments) to the app's name now.
func (p *Platform) currentName(name string) string {
	for i := 0; i < 10 && name != ""; i++ {
		var exists int
		if p.db.QueryRow(`SELECT 1 FROM apps WHERE name = ?`, name).Scan(&exists) == nil {
			return name
		}
		var next string
		if p.db.QueryRow(`SELECT new FROM app_renames WHERE old = ?`, name).Scan(&next) != nil {
			return name
		}
		name = next
	}
	return name
}

// rowQuerier is *sql.DB or *sql.Tx: the platform database allows one
// connection, so code inside a transaction must query through it.
type rowQuerier interface {
	QueryRow(query string, args ...any) *sql.Row
}

func bareHost(hostport string) string {
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		return h
	}
	return hostport
}

// AppsDomain is the domain apps are served under, e.g. renoxium.app.
func (p *Platform) AppsDomain() string { return p.cfg.AppsDomain }
