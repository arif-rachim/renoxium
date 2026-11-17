package platform

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/arif-rachim/renoxium/internal/cloudflare"
)

// CustomDomain is an app's own domain and its setup state.
type CustomDomain struct {
	Host       string
	Status     string // pending until DNS and the certificate are in place, then active
	CNAME      string // where the owner points the domain
	Ownership  struct{ Name, Value string }
	SSLRecords []struct{ Name, Value string }
	Problems   []string
}

var hostRE = regexp.MustCompile(`^([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]{2,63}$`)

func (p *Platform) ensureDomainTable() error {
	_, err := p.db.Exec(`CREATE TABLE IF NOT EXISTS custom_domains (app TEXT PRIMARY KEY, host TEXT NOT NULL UNIQUE, cf_id TEXT NOT NULL,
		status TEXT NOT NULL, details TEXT NOT NULL DEFAULT '{}', created_at TEXT NOT NULL DEFAULT (datetime('now')))`)
	if err != nil {
		return err
	}
	// Active custom domains serve live and are the app's main address.
	rows, err := p.db.Query(`SELECT app, host FROM custom_domains WHERE status = 'active'`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var app, host string
		rows.Scan(&app, &host)
		p.liveHosts.Store(app, host)
	}
	return rows.Err()
}

// CustomDomainsAvailable reports whether this server can set up domains.
func (p *Platform) CustomDomainsAvailable() bool {
	return p.cfg.Cloudflare != nil && p.cfg.CustomDomainTarget != ""
}

// CustomDomainAllowed reports whether the app's owner's plan includes it.
func (p *Platform) CustomDomainAllowed(app string) bool {
	return p.cfg.OwnerPlan == nil || p.planOf(p.ownerOf(app)) != planFree
}

// CustomDomain returns the app's domain, or nil.
func (p *Platform) CustomDomain(app string) (*CustomDomain, error) {
	var d CustomDomain
	var details string
	err := p.db.QueryRow(`SELECT host, status, details FROM custom_domains WHERE app = ?`, app).Scan(&d.Host, &d.Status, &details)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	json.Unmarshal([]byte(details), &d)
	d.CNAME = p.cfg.CustomDomainTarget
	return &d, nil
}

func (p *Platform) saveDomain(app string, h cloudflare.Hostname) (*CustomDomain, error) {
	d := CustomDomain{Host: h.Hostname, Status: "pending", Problems: h.Problems()}
	if h.Active() {
		d.Status = "active"
	}
	d.Ownership.Name, d.Ownership.Value = h.Ownership.Name, h.Ownership.Value
	for _, r := range h.SSL.ValidationRecords {
		if r.TxtName != "" {
			d.SSLRecords = append(d.SSLRecords, struct{ Name, Value string }{r.TxtName, r.TxtValue})
		}
	}
	details, _ := json.Marshal(d)
	if _, err := p.db.Exec(`UPDATE custom_domains SET status = ?, details = ? WHERE app = ?`, d.Status, string(details), app); err != nil {
		return nil, err
	}
	if d.Status == "active" {
		if _, err := p.db.Exec(`INSERT OR IGNORE INTO domains (host, app, stage) VALUES (?, ?, ?)`, d.Host, app, Live); err != nil {
			return nil, err
		}
		p.liveHosts.Store(app, d.Host)
	}
	d.CNAME = p.cfg.CustomDomainTarget
	return &d, nil
}

// SetCustomDomain registers a domain for an app with Cloudflare. It serves
// the app once its DNS points here and the certificate is issued.
func (p *Platform) SetCustomDomain(ctx context.Context, app, host string) (*CustomDomain, error) {
	if !p.CustomDomainsAvailable() {
		return nil, errors.New("custom domains are not available on this Renoxium server yet")
	}
	if !p.CustomDomainAllowed(app) {
		return nil, fmt.Errorf("custom domains need the Pro or Business plan (%s/billing)", p.cfg.SiteURL)
	}
	host = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(host)), ".")
	host = strings.TrimPrefix(strings.TrimPrefix(host, "https://"), "http://")
	host = strings.TrimSuffix(host, "/")
	switch {
	case !hostRE.MatchString(host) || len(host) > 253:
		return nil, fmt.Errorf("%q is not a domain name; enter it like www.yourshop.com", host)
	case host == p.cfg.AppsDomain || strings.HasSuffix(host, "."+p.cfg.AppsDomain) || strings.Contains(p.cfg.SiteURL, "://"+host) ||
		strings.HasSuffix(host, ".renoxium.com") || host == "renoxium.com":
		return nil, errors.New("use a domain you own, not a Renoxium address")
	}
	if existing, err := p.CustomDomain(app); err != nil || existing != nil {
		if err == nil {
			err = errors.New("this app already has a domain; remove it first")
		}
		return nil, err
	}
	var taken int
	p.db.QueryRow(`SELECT count(*) FROM domains WHERE host = ?`, host).Scan(&taken)
	if taken > 0 {
		return nil, errors.New("this domain is already used by an app")
	}
	h, err := p.cfg.Cloudflare.Create(ctx, host)
	if err != nil {
		return nil, err
	}
	if _, err := p.db.Exec(`INSERT INTO custom_domains (app, host, cf_id, status) VALUES (?, ?, ?, 'pending')`, app, host, h.ID); err != nil {
		p.cfg.Cloudflare.Delete(context.WithoutCancel(ctx), h.ID)
		return nil, errors.New("this domain is already used by an app")
	}
	return p.saveDomain(app, h)
}

// RefreshCustomDomain reads the domain's state from Cloudflare and starts
// serving it once it is active.
func (p *Platform) RefreshCustomDomain(ctx context.Context, app string) (*CustomDomain, error) {
	var id string
	if err := p.db.QueryRow(`SELECT cf_id FROM custom_domains WHERE app = ?`, app).Scan(&id); err != nil || p.cfg.Cloudflare == nil {
		return p.CustomDomain(app)
	}
	h, err := p.cfg.Cloudflare.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	return p.saveDomain(app, h)
}

// RemoveCustomDomain stops serving the domain and removes it at Cloudflare.
func (p *Platform) RemoveCustomDomain(ctx context.Context, app string) error {
	var id, host string
	if err := p.db.QueryRow(`SELECT cf_id, host FROM custom_domains WHERE app = ?`, app).Scan(&id, &host); err != nil {
		return nil
	}
	if p.cfg.Cloudflare != nil {
		if err := p.cfg.Cloudflare.Delete(ctx, id); err != nil && !strings.Contains(err.Error(), "not found") {
			return err
		}
	}
	p.db.Exec(`DELETE FROM domains WHERE host = ? AND app = ?`, host, app)
	p.db.Exec(`DELETE FROM custom_domains WHERE app = ?`, app)
	p.hosts.Delete(host)
	p.liveHosts.Delete(app)
	return nil
}
