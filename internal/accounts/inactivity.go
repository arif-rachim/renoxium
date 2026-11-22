package accounts

import (
	"database/sql"
	"errors"
	"time"
)

// Accounts without activity for InactiveMonths are deactivated: their live
// apps show a "paused" page until the owner signs in and reactivates. The
// owner is emailed WarnDays before. Nothing is deleted. Accounts on a paid or
// granted plan, and accounts with a paid work app, are exempt (the check that
// knows about work apps lives in the web package).
//
// Activity is the owner or a staff member of one of the account's apps
// using the signed-in Renoxium site, an agent calling the MCP server for the
// account, a publish, or a paid card payment through Renoxium's checkout in
// a live app of the account (see NoteActivity and its callers).
const (
	InactiveMonths = 6
	WarnDays       = 30
)

// activityEvery is how often NoteActivity writes for one account: activity
// within the hour is the same activity for a six-month rule.
const activityEvery = time.Hour

// DeactivationDue is when an account last active at lastActive is
// deactivated, if nothing happens before.
func DeactivationDue(lastActive time.Time) time.Time { return lastActive.AddDate(0, InactiveMonths, 0) }

// Activity is an account's activity state.
type Activity struct {
	UserID, Email string
	ActiveAt      time.Time // last activity (sign-up when there was none)
	WarnedAt      time.Time // zero: no warning sent since the last activity
	DeactivateOn  time.Time // the date promised in the warning
	DeactivatedAt time.Time // zero: active
}

// Deactivated reports whether the account is deactivated.
func (a Activity) Deactivated() bool { return !a.DeactivatedAt.IsZero() }

func (s *Store) ensureActivity() error {
	added := false
	for _, col := range []string{"active_at", "warned_at", "deactivate_on", "deactivated_at"} {
		_, err := s.db.Exec(`ALTER TABLE users ADD COLUMN ` + col + ` INTEGER NOT NULL DEFAULT 0`)
		if err != nil && !isDuplicateColumn(err) {
			return err
		}
		if err == nil && col == "active_at" {
			added = true
		}
	}
	if !added {
		return nil
	}
	// Accounts from before activity was recorded: the latest of sign-up, the
	// last browser session started and the last agent token issued. The
	// warning always comes WarnDays before deactivation, so an account whose
	// activity was missed here is still told first.
	_, err := s.db.Exec(`UPDATE users SET active_at = max(
		coalesce(unixepoch(created_at), 0),
		coalesce((SELECT max(expires_at) FROM sessions WHERE user_id = users.id), 0) - ?,
		coalesce((SELECT max(access_expires_at) FROM tokens WHERE user_id = users.id), 0) - ?)`,
		int64(SessionTTL.Seconds()), int64(AccessTokenTTL.Seconds()))
	return err
}

// SetClock replaces the store's clock. Tests only.
func (s *Store) SetClock(now func() time.Time) { s.now = now }

// NoteActivity records activity of an account. It clears a pending warning
// but does not reactivate a deactivated account: the owner does that with
// Reactivate. It reports whether anything was written (at most once per
// activityEvery, unless a warning was pending).
func (s *Store) NoteActivity(userID string) bool {
	now := s.now().Unix()
	res, err := s.db.Exec(`UPDATE users SET active_at = ?, warned_at = 0, deactivate_on = 0
		WHERE id = ? AND (active_at <= ? OR warned_at != 0)`, now, userID, now-int64(activityEvery.Seconds()))
	if err != nil {
		return false
	}
	n, _ := res.RowsAffected()
	return n > 0
}

const activityColumns = `id, email, active_at, warned_at, deactivate_on, deactivated_at`

func scanActivity(row interface{ Scan(...any) error }) (Activity, error) {
	var a Activity
	var active, warned, on, deact int64
	if err := row.Scan(&a.UserID, &a.Email, &active, &warned, &on, &deact); err != nil {
		return Activity{}, err
	}
	a.ActiveAt, a.WarnedAt, a.DeactivateOn, a.DeactivatedAt = unixOrZero(active), unixOrZero(warned), unixOrZero(on), unixOrZero(deact)
	return a, nil
}

func unixOrZero(n int64) time.Time {
	if n <= 0 {
		return time.Time{}
	}
	return time.Unix(n, 0)
}

// Activity returns an account's activity state.
func (s *Store) Activity(userID string) (Activity, error) {
	a, err := scanActivity(s.db.QueryRow(`SELECT `+activityColumns+` FROM users WHERE id = ?`, userID))
	if errors.Is(err, sql.ErrNoRows) {
		return Activity{}, ErrNotFound
	}
	return a, err
}

func (s *Store) queryActivity(where string, args ...any) ([]Activity, error) {
	rows, err := s.db.Query(`SELECT `+activityColumns+` FROM users WHERE `+where+` ORDER BY active_at`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Activity
	for rows.Next() {
		a, err := scanActivity(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// IdleAccounts lists active (not deactivated) accounts whose last activity
// is before the given time.
func (s *Store) IdleAccounts(before time.Time) ([]Activity, error) {
	return s.queryActivity(`deactivated_at = 0 AND active_at < ?`, before.Unix())
}

// DeactivatedAccounts lists deactivated accounts, longest idle first.
func (s *Store) DeactivatedAccounts() ([]Activity, error) {
	return s.queryActivity(`deactivated_at != 0`)
}

// MarkWarned records that the deactivation warning was sent, promising
// deactivation on the given date. It only writes when no warning is pending
// and nothing happened since lastActive, and reports whether it wrote.
func (s *Store) MarkWarned(userID string, lastActive, on time.Time) (bool, error) {
	res, err := s.db.Exec(`UPDATE users SET warned_at = ?, deactivate_on = ? WHERE id = ? AND warned_at = 0 AND deactivated_at = 0 AND active_at = ?`,
		s.now().Unix(), on.Unix(), userID, lastActive.Unix())
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// ClearWarning forgets a pending warning, for an account that became exempt.
func (s *Store) ClearWarning(userID string) error {
	_, err := s.db.Exec(`UPDATE users SET warned_at = 0, deactivate_on = 0 WHERE id = ?`, userID)
	return err
}

// Deactivate deactivates a warned account whose promised date has come. It
// does nothing when the owner was active since the warning, and reports
// whether it deactivated.
func (s *Store) Deactivate(userID string) (bool, error) {
	now := s.now().Unix()
	res, err := s.db.Exec(`UPDATE users SET deactivated_at = ? WHERE id = ? AND deactivated_at = 0 AND warned_at != 0 AND deactivate_on <= ?`,
		now, userID, now)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// Reactivate brings a deactivated account back at once. It counts as
// activity.
func (s *Store) Reactivate(userID string) error {
	res, err := s.db.Exec(`UPDATE users SET deactivated_at = 0, warned_at = 0, deactivate_on = 0, active_at = ? WHERE id = ?`, s.now().Unix(), userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// IsDeactivated reports whether an account is deactivated.
func (s *Store) IsDeactivated(userID string) bool {
	var n int64
	s.db.QueryRow(`SELECT deactivated_at FROM users WHERE id = ?`, userID).Scan(&n)
	return n > 0
}
