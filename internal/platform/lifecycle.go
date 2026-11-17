package platform

import (
	"archive/zip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/arif-rachim/renoxium/internal/tenantdb"
)

// DeleteApp removes an app for good: its versions, hosts, Stripe link,
// databases, logs and backups. Uploaded files shared with other apps stay
// on disk; only this app's record of them goes.
func (p *Platform) DeleteApp(app string) error {
	if _, err := p.Info(app); err != nil {
		return err
	}
	p.change.Lock()
	defer p.change.Unlock()
	p.dropStage(app, Live)
	p.dropStage(app, Preview)
	p.mu.Lock()
	if l := p.logs[app]; l != nil {
		l.Close()
		delete(p.logs, app)
	}
	p.mu.Unlock()
	rows, _ := p.db.Query(`SELECT host FROM domains WHERE app = ?`, app)
	var hosts []string
	for rows != nil && rows.Next() {
		var h string
		rows.Scan(&h)
		hosts = append(hosts, h)
	}
	if rows != nil {
		rows.Close()
	}
	tx, err := p.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, stmt := range []string{
		`DELETE FROM versions WHERE app = ?`, `DELETE FROM domains WHERE app = ?`, `DELETE FROM payment_accounts WHERE app = ?`,
		`DELETE FROM app_files WHERE app = ?`, `DELETE FROM app_staff WHERE app = ?`, `DELETE FROM apps WHERE name = ?`,
	} {
		if _, err := tx.Exec(stmt, app); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	for _, h := range hosts {
		p.hosts.Delete(h)
	}
	p.owners.Delete(app)
	return os.RemoveAll(p.appDir(app))
}

// ExportApp writes an app into a zip: its newest definition, a consistent
// copy of its live database, and the list of its uploaded files.
func (p *Platform) ExportApp(ctx context.Context, app string, zw *zip.Writer) error {
	info, err := p.Info(app)
	if err != nil {
		return err
	}
	add := func(name string, v any) error {
		w, err := zw.Create(app + "/" + name)
		if err != nil {
			return err
		}
		enc := json.NewEncoder(w)
		enc.SetIndent("", " ")
		return enc.Encode(v)
	}
	if err := add("app.json", info); err != nil {
		return err
	}
	if def, n, err := p.Definition(app, 0); err == nil {
		if err := add(fmt.Sprintf("definition-v%d.json", n), def); err != nil {
			return err
		}
	}
	if files, err := p.Files(app); err == nil {
		if err := add("files.json", files); err != nil {
			return err
		}
	}
	live := p.dbPath(app, Live)
	if _, err := os.Stat(live); err != nil {
		return nil // never published: no live data
	}
	tmp, err := os.MkdirTemp("", "rx-export-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	copyPath := filepath.Join(tmp, "live.db")
	if err := tenantdb.Snapshot(live, copyPath); err != nil {
		return err
	}
	f, err := os.Open(copyPath)
	if err != nil {
		return err
	}
	defer f.Close()
	w, err := zw.Create(app + "/live.db")
	if err != nil {
		return err
	}
	_, err = io.Copy(w, f)
	return err
}
