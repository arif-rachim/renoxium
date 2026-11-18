package platform

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// VisitorRow is one visitor account as the owner sees it. Password hashes
// never leave the platform.
type VisitorRow struct {
	ID        int64
	Email     string
	Phone     string
	Name      string
	Verified  bool
	CreatedAt string
}

// Visitors lists one page of an app's live visitor accounts, newest first.
func (p *Platform) Visitors(ctx context.Context, app, search string, page int) ([]VisitorRow, int, error) {
	st, err := p.loadStage(app, Live)
	if err != nil || st == nil {
		return nil, 0, err
	}
	where, args := "", []any{}
	if search = strings.TrimSpace(search); search != "" {
		where = ` WHERE email LIKE ? ESCAPE '\' OR name LIKE ? ESCAPE '\' OR phone LIKE ? ESCAPE '\'`
		like := "%" + likeEscape(search) + "%"
		args = append(args, like, like, like)
	}
	var total int
	if err := st.admin.QueryRowContext(ctx, "SELECT count(*) FROM rx_users"+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	if page < 1 {
		page = 1
	}
	rows, err := st.admin.QueryContext(ctx, "SELECT id, coalesce(email, ''), coalesce(phone, ''), name, verified_at, created_at FROM rx_users"+where+" ORDER BY id DESC LIMIT ? OFFSET ?",
		append(args, AdminPageSize, (page-1)*AdminPageSize)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []VisitorRow{}
	for rows.Next() {
		var v VisitorRow
		var verified sql.NullString
		if err := rows.Scan(&v.ID, &v.Email, &v.Phone, &v.Name, &verified, &v.CreatedAt); err != nil {
			return nil, 0, err
		}
		v.Verified = verified.Valid
		out = append(out, v)
	}
	return out, total, rows.Err()
}

// DeleteVisitor removes a visitor account and signs it out. Rows the app
// stored for the visitor (such as orders) are kept.
func (p *Platform) DeleteVisitor(ctx context.Context, app string, id int64, by string) error {
	st, err := p.loadStage(app, Live)
	if err != nil {
		return err
	}
	if st == nil {
		return fmt.Errorf("app %q is not published yet", app)
	}
	res, err := st.admin.ExecContext(ctx, `DELETE FROM rx_users WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("visitor %d: %w", id, ErrNotFound)
	}
	p.event(st, "admin", "warn", "", fmt.Sprintf("%s deleted visitor account #%d", by, id))
	return nil
}

// Cleanup deletes expired visitor sessions, links and codes of every
// running stage. Call it periodically.
func (p *Platform) Cleanup(ctx context.Context) {
	p.cleanDurable()
	p.forgetLive("")
	// Domain claims nobody proved within a week are dropped.
	p.db.Exec(`DELETE FROM custom_domains WHERE status = 'verify' AND created_at < datetime('now', '-7 days')`)
	p.mu.Lock()
	stages := make([]*stage, 0, len(p.stages))
	for _, st := range p.stages {
		stages = append(stages, st)
	}
	p.mu.Unlock()
	now := time.Now().Unix()
	for _, st := range stages {
		st.admin.ExecContext(ctx, `DELETE FROM rx_sessions WHERE expires_at < ?`, now)
		st.admin.ExecContext(ctx, `DELETE FROM rx_email_tokens WHERE expires_at < ? OR used = 1`, now)
		st.admin.ExecContext(ctx, `DELETE FROM rx_phone_codes WHERE expires_at < ?`, now)
	}
	p.mu.Lock()
	for k, e := range p.rids {
		if time.Now().After(e.expires) {
			delete(p.rids, k)
		}
	}
	for k, w := range p.limits {
		if time.Since(w.start) > w.period {
			delete(p.limits, k)
		}
	}
	p.mu.Unlock()
}
