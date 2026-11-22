package platform

import (
	"archive/zip"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/arif-rachim/renoxium/internal/appdef"
	"github.com/arif-rachim/renoxium/internal/licence"
	"github.com/arif-rachim/renoxium/internal/stripe"
)

// LicenceKeys, when set, replace licence.PublicKeys for checking the
// licence of an imported app (tests sign with a key of their own).
var LicenceKeys []ed25519.PublicKey

var definitionRE = regexp.MustCompile(`^definition-v(\d+)\.json$`)

// ImportApp creates an app from an export (ExportApp) for owner: its newest
// definition, live data, uploaded files and visitors' private files, at the
// same addresses as before. The export must carry a licence for a paid plan
// (internal/licence); which app is taken from want, or the only app in the
// export when want is empty. The app is live once this returns.
func (p *Platform) ImportApp(ctx context.Context, owner string, zr *zip.Reader, want string) (string, licence.Licence, error) {
	entries := map[string]*zip.File{}
	var apps []string
	for _, f := range zr.File {
		entries[f.Name] = f
		if dir, name := path.Split(f.Name); name == "app.json" && strings.Count(dir, "/") == 1 {
			apps = append(apps, strings.TrimSuffix(dir, "/"))
		}
	}
	sort.Strings(apps)
	app := want
	switch {
	case want == "" && len(apps) == 1:
		app = apps[0]
	case want == "":
		return "", licence.Licence{}, fmt.Errorf("the export holds %d apps (%s); name the one to import", len(apps), strings.Join(apps, ", "))
	case entries[want+"/app.json"] == nil:
		return "", licence.Licence{}, fmt.Errorf("the export has no app %q (it has %s)", want, strings.Join(apps, ", "))
	}
	read := func(name string) ([]byte, error) {
		f := entries[app+"/"+name]
		if f == nil {
			return nil, os.ErrNotExist
		}
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		defer rc.Close()
		return io.ReadAll(io.LimitReader(rc, 64<<20))
	}

	raw, _ := read("licence.json")
	lic, err := licence.Verify(raw, LicenceKeys...)
	if err != nil {
		return "", lic, err
	}
	if lic.App != app {
		return "", lic, fmt.Errorf("the licence is for the app %q, not %q", lic.App, app)
	}

	newest := 0
	for name := range entries {
		if m := definitionRE.FindStringSubmatch(strings.TrimPrefix(name, app+"/")); m != nil && strings.HasPrefix(name, app+"/") {
			if n, _ := strconv.Atoi(m[1]); n > newest {
				newest = n
			}
		}
	}
	if newest == 0 {
		return "", lic, errors.New("the export has no definition of the app")
	}
	rawDef, err := read(fmt.Sprintf("definition-v%d.json", newest))
	if err != nil {
		return "", lic, err
	}
	def, problems := appdef.Parse(rawDef)
	if len(problems) > 0 {
		return "", lic, fmt.Errorf("the app's definition: %s", strings.Join(problems, "; "))
	}

	if _, err := p.CreateApp(owner, app); err != nil {
		return "", lic, err
	}
	if res, err := p.ApplyChange(ctx, app, def); err != nil || !res.OK {
		if err == nil {
			err = fmt.Errorf("the app does not pass its checks: %v", res.Problems)
		}
		return "", lic, err
	}
	// The live data goes in place before publishing, which then only adds
	// what the definition has and the data lacks.
	if f := entries[app+"/live.db"]; f != nil {
		if err := writeEntry(f, p.dbPath(app, Live)); err != nil {
			return "", lic, err
		}
	}
	if _, err := p.Publish(app, 0); err != nil {
		return "", lic, err
	}

	for name, f := range entries {
		file, ok := strings.CutPrefix(name, app+"/files/")
		if !ok || strings.Contains(file, "/") {
			continue
		}
		hash, _, _ := strings.Cut(file, ".")
		rc, err := f.Open()
		if err != nil {
			return "", lic, err
		}
		b, err := p.files.PutAs(hash, rc)
		rc.Close()
		if err != nil {
			return "", lic, fmt.Errorf("file %s: %w", file, err)
		}
		if _, err := p.db.Exec(`INSERT OR IGNORE INTO blobs (hash, size, content_type, created_at, fitted) VALUES (?, ?, ?, ?, 1)`,
			b.Hash, b.Size, b.ContentType, now()); err != nil {
			return "", lic, err
		}
		if _, err := p.db.Exec(`INSERT OR IGNORE INTO app_files (app, hash, created_at) VALUES (?, ?, ?)`, app, b.Hash, now()); err != nil {
			return "", lic, err
		}
	}

	if raw, err := read("private-files.json"); err == nil {
		var list []PrivateFile
		if err := json.Unmarshal(raw, &list); err != nil {
			return "", lic, fmt.Errorf("private-files.json: %w", err)
		}
		for _, pf := range list {
			f := entries[app+"/private-files/"+pf.ID+"/"+pf.Name]
			if f == nil || strings.ContainsAny(pf.ID, `/\.`) {
				continue
			}
			if err := writeEntry(f, filepath.Join(p.privateDir(app, Live), pf.ID)); err != nil {
				return "", lic, err
			}
			if _, err := p.db.Exec(`INSERT OR IGNORE INTO private_files (id, app, stage, user_id, name, size, content_type, created_at)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, pf.ID, app, Live, pf.UserID, pf.Name, pf.Size, pf.ContentType, pf.CreatedAt); err != nil {
				return "", lic, err
			}
		}
	}

	if p.cfg.SelfHosted {
		// Card payments go to the Stripe key's own account.
		if _, err := p.db.Exec(`INSERT OR REPLACE INTO payment_accounts (app, stripe_account, charges_enabled, details_submitted, updated_at)
			VALUES (?, ?, 1, 1, ?)`, app, stripe.DirectAccount, now()); err != nil {
			return "", lic, err
		}
	}
	return app, lic, nil
}

// writeEntry copies a zip entry to a new file at dst.
func writeEntry(f *zip.File, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, rc); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// UseOwnDomain serves an app's live stage on host as its main address, for
// an app on its owner's server, where the owner points the domain at the
// server and its proxy holds the certificate (no Cloudflare).
func (p *Platform) UseOwnDomain(app, host string) error {
	host = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(host)), ".")
	if !hostRE.MatchString(host) {
		return fmt.Errorf("%q is not a domain name; enter it like www.yourshop.com", host)
	}
	if _, err := p.Info(app); err != nil {
		return err
	}
	if _, err := p.db.Exec(`INSERT OR REPLACE INTO custom_domains (app, host, cf_id, status) VALUES (?, ?, '', 'active')`, app, host); err != nil {
		return err
	}
	if _, err := p.db.Exec(`INSERT OR REPLACE INTO domains (host, app, stage) VALUES (?, ?, ?)`, host, app, Live); err != nil {
		return err
	}
	p.hosts.Delete(host)
	p.liveHosts.Store(app, host)
	return nil
}
