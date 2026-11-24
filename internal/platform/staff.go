package platform

import (
	"errors"
	"fmt"
	"strings"
)

// GrantedStaff is how many staff members each app of a granted account (or
// of a server without plans) may have. On free and member accounts staff
// come with the app's kind: ShopStaff for a shop, WorkAppUsers (up to
// WorkAppMaxUsers, paid per seat) for a work app.
const GrantedStaff = 10

func (p *Platform) ensureStaffTable() error {
	_, err := p.db.Exec(`CREATE TABLE IF NOT EXISTS app_staff (app TEXT NOT NULL, email TEXT NOT NULL, added_by TEXT NOT NULL,
		created_at TEXT NOT NULL DEFAULT (datetime('now')), PRIMARY KEY (app, email))`)
	return err
}

// Staff lists the emails with staff access to an app.
func (p *Platform) Staff(app string) ([]string, error) {
	rows, err := p.db.Query(`SELECT email FROM app_staff WHERE app = ? ORDER BY created_at, email`, app)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var e string
		if err := rows.Scan(&e); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// StaffLimit is how many staff members the app may have.
func (p *Platform) StaffLimit(app string) int {
	if p.cfg.OwnerPlan == nil || p.granted(app) {
		return GrantedStaff
	}
	switch {
	case p.workPaid(app) && p.cfg.WorkSeats:
		return WorkAppMaxUsers
	case p.workPaid(app):
		return WorkAppUsers
	case p.appKind(app) == KindShop:
		return ShopStaff
	}
	return 0
}

// AddStaff gives an email staff access to an app, within the plan's limit.
func (p *Platform) AddStaff(app, email, by string) error {
	email = strings.ToLower(strings.TrimSpace(email))
	if _, ok := normalizeEmail(email); !ok {
		return errors.New("enter a valid email address")
	}
	staff, err := p.Staff(app)
	if err != nil {
		return err
	}
	for _, s := range staff {
		if s == email {
			return nil
		}
	}
	if limit := p.StaffLimit(app); len(staff) >= limit {
		if limit == 0 {
			return fmt.Errorf("staff accounts come with a shop or a work app; choose one at %s/apps/%s/plan", p.cfg.SiteURL, app)
		}
		if limit == WorkAppMaxUsers && p.workPaid(app) {
			return fmt.Errorf("a work app may have at most %d staff members for now", limit)
		}
		if p.appKind(app) == KindShop {
			return fmt.Errorf("a shop may have %d staff members; for more, make %s a work app at %s/apps/%s/plan", limit, app, p.cfg.SiteURL, app)
		}
		return fmt.Errorf("this app may have %d staff members; for more, contact Renoxium at %s/support", limit, p.cfg.SiteURL)
	}
	_, err = p.db.Exec(`INSERT OR IGNORE INTO app_staff (app, email, added_by) VALUES (?, ?, ?)`, app, email, by)
	return err
}

// RemoveStaff ends an email's staff access.
func (p *Platform) RemoveStaff(app, email string) error {
	_, err := p.db.Exec(`DELETE FROM app_staff WHERE app = ? AND email = ?`, app, strings.ToLower(email))
	return err
}

// IsStaff reports whether an email has staff access to an app.
func (p *Platform) IsStaff(app, email string) bool {
	var n int
	p.db.QueryRow(`SELECT count(*) FROM app_staff WHERE app = ? AND email = ?`, app, strings.ToLower(email)).Scan(&n)
	return n > 0
}

// StaffApps lists the apps an email has staff access to.
func (p *Platform) StaffApps(email string) ([]AppInfo, error) {
	rows, err := p.db.Query(`SELECT app FROM app_staff WHERE email = ? ORDER BY app`, strings.ToLower(email))
	if err != nil {
		return nil, err
	}
	var names []string
	for rows.Next() {
		var n string
		rows.Scan(&n)
		names = append(names, n)
	}
	rows.Close()
	out := []AppInfo{}
	for _, n := range names {
		if info, err := p.Info(n); err == nil {
			out = append(out, info)
		}
	}
	return out, nil
}
