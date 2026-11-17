package platform

import (
	"database/sql"
	"strings"
)

// AppRow is one app as the Renoxium operator sees it.
type AppRow struct {
	Name           string
	Owner          string
	LiveVersion    int
	PreviewVersion int
	Suspended      string
	CreatedAt      string
	StripeAccount  string
	ChargesEnabled bool
}

// AllApps lists apps for the operator, newest first, optionally filtered by
// name or owner ID.
func (p *Platform) AllApps(search string, limit int) ([]AppRow, error) {
	q := `SELECT a.name, a.owner, a.live_version, a.preview_version, a.suspended, a.created_at,
		coalesce(pa.stripe_account, ''), coalesce(pa.charges_enabled, 0)
		FROM apps a LEFT JOIN payment_accounts pa ON pa.app = a.name`
	var args []any
	if search = strings.TrimSpace(search); search != "" {
		q += ` WHERE a.name LIKE ? ESCAPE '\' OR a.owner = ?`
		args = append(args, "%"+likeEscape(search)+"%", search)
	}
	q += ` ORDER BY a.created_at DESC LIMIT ?`
	args = append(args, limit)
	rows, err := p.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AppRow{}
	for rows.Next() {
		var a AppRow
		if err := rows.Scan(&a.Name, &a.Owner, &a.LiveVersion, &a.PreviewVersion, &a.Suspended, &a.CreatedAt, &a.StripeAccount, &a.ChargesEnabled); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// PlatformStats are totals for the operator overview.
type PlatformStats struct {
	Apps, LiveApps, SuspendedApps, StripeReady, WhatsAppCodes int
}

// Stats counts apps and this month's WhatsApp codes.
func (p *Platform) Stats() (PlatformStats, error) {
	var s PlatformStats
	err := p.db.QueryRow(`SELECT count(*), coalesce(sum(live_version > 0), 0), coalesce(sum(suspended != ''), 0) FROM apps`).Scan(&s.Apps, &s.LiveApps, &s.SuspendedApps)
	if err != nil {
		return s, err
	}
	p.db.QueryRow(`SELECT count(*) FROM payment_accounts WHERE charges_enabled = 1`).Scan(&s.StripeReady)
	p.db.QueryRow(`SELECT coalesce(sum(count), 0) FROM usage WHERE kind = 'whatsapp' AND month = strftime('%Y-%m', 'now')`).Scan(&s.WhatsAppCodes)
	return s, nil
}

// AbuseReport is a report sent through /abuse.
type AbuseReport struct {
	ID        int64
	Address   string
	Contact   string
	Details   string
	CreatedAt string
	Resolved  bool
}

func (p *Platform) ensureAbuseTable() error {
	_, err := p.db.Exec(`CREATE TABLE IF NOT EXISTS abuse_reports (id INTEGER PRIMARY KEY, address TEXT NOT NULL, contact TEXT NOT NULL,
		details TEXT NOT NULL, created_at TEXT NOT NULL DEFAULT (datetime('now')), resolved INTEGER NOT NULL DEFAULT 0)`)
	return err
}

// RecordAbuseReport stores a report for the operator.
func (p *Platform) RecordAbuseReport(address, contact, details string) error {
	_, err := p.db.Exec(`INSERT INTO abuse_reports (address, contact, details) VALUES (?, ?, ?)`, address, contact, details)
	return err
}

// AbuseReports lists reports, open ones first.
func (p *Platform) AbuseReports(limit int) ([]AbuseReport, error) {
	rows, err := p.db.Query(`SELECT id, address, contact, details, created_at, resolved FROM abuse_reports ORDER BY resolved, id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AbuseReport{}
	for rows.Next() {
		var a AbuseReport
		if err := rows.Scan(&a.ID, &a.Address, &a.Contact, &a.Details, &a.CreatedAt, &a.Resolved); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// ResolveAbuseReport marks a report handled.
func (p *Platform) ResolveAbuseReport(id int64) error {
	res, err := p.db.Exec(`UPDATE abuse_reports SET resolved = 1 WHERE id = ?`, id)
	if err == nil {
		if n, _ := res.RowsAffected(); n == 0 {
			err = sql.ErrNoRows
		}
	}
	return err
}
