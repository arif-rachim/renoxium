package platform

import (
	"archive/zip"
	"context"
	"encoding/json"
	"fmt"
	"github.com/arif-rachim/renoxium/internal/licence"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/arif-rachim/renoxium/internal/tenantdb"
)

// DeleteApp removes an app for good: its versions, hosts, Stripe link,
// databases, logs and backups. Uploaded files shared with other apps stay
// on disk; only this app's record of them goes.
func (p *Platform) DeleteApp(app string) error {
	if _, err := p.Info(app); err != nil {
		return err
	}
	if err := p.RemoveCustomDomain(context.Background(), app); err != nil {
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
	if _, err := tx.Exec(`INSERT OR REPLACE INTO released_names (name, owner, released_at) SELECT name, owner, ? FROM apps WHERE name = ?`,
		time.Now().Unix(), app); err != nil {
		return err
	}
	for _, stmt := range []string{
		`DELETE FROM versions WHERE app = ?`, `DELETE FROM domains WHERE app = ?`, `DELETE FROM payment_accounts WHERE app = ?`,
		`DELETE FROM app_files WHERE app = ?`, `DELETE FROM app_staff WHERE app = ?`, `DELETE FROM owner_alerts WHERE app = ?`, `DELETE FROM job_runs WHERE app = ?`, `DELETE FROM app_hosts WHERE app = ?`,
		`DELETE FROM app_secrets WHERE app = ?`, `DELETE FROM private_files WHERE app = ?`, `DELETE FROM preview_edits WHERE app = ?`,
		`DELETE FROM apps WHERE name = ?`,
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
	p.forgetLive(app)
	return os.RemoveAll(p.appDir(app))
}

// ExportApp writes an app into a zip: its newest definition, a consistent
// copy of its live database, and its uploaded files with their list.
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
	// A granted account's export, or a paid work app's, may run on its
	// owner's own server: it carries a licence signed by this server
	// (internal/licence). A shop's does not: it would no longer pay the shop
	// fee. The licence names the former plans ("business" for a grant, "pro"
	// for a work app) so that self-hosted servers of any version accept it.
	if p.cfg.LicenceKey != nil {
		owner := p.ownerOf(app)
		plan := ""
		switch {
		case p.planOf(owner) == planGranted:
			plan = "business"
		case p.workPaid(app):
			plan = "pro"
		}
		if licence.SelfHostPlans[plan] {
			email := ""
			if p.cfg.OwnerEmail != nil {
				email, _ = p.cfg.OwnerEmail(owner)
			}
			raw, err := licence.Sign(p.cfg.LicenceKey, licence.Licence{App: app, Account: owner, Email: email, Plan: plan})
			if err != nil {
				return err
			}
			w, err := zw.Create(app + "/licence.json")
			if err != nil {
				return err
			}
			if _, err := w.Write(raw); err != nil {
				return err
			}
		}
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
		// The files themselves, under the name their address ends with, so the
		// export is everything the app needs to run somewhere else.
		for _, fi := range files {
			name := strings.TrimPrefix(fi.URL, FilesPrefix)
			hash, _, _ := strings.Cut(name, ".")
			fh, err := p.files.Open(hash)
			if err != nil {
				continue // a file removed while exporting
			}
			w, err := zw.Create(app + "/files/" + name)
			if err == nil {
				_, err = io.Copy(w, fh)
			}
			fh.Close()
			if err != nil {
				return err
			}
		}
	}
	if err := p.exportPrivateFiles(app, zw, add); err != nil {
		return err
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
	if err := tenantdb.ScrubForExport(copyPath); err != nil {
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
