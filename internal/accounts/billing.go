package accounts

import (
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/arif-rachim/renoxium/internal/password"
)

// Plans. An account without a paying subscription is on PlanFree.
const (
	PlanFree     = "free"
	PlanPro      = "pro"
	PlanBusiness = "business"
)

// Billing is an account's Renoxium subscription as last seen from Stripe.
type Billing struct {
	Customer          string // Stripe customer on the platform account, cus_...
	Plan              string
	Status            string // Stripe subscription status, "" when none
	Subscription      string
	PeriodEnd         time.Time
	CancelAtPeriodEnd bool
	Yearly            bool // paid by the year, not the month
}

func (s *Store) ensureBilling() error {
	_, err := s.db.Exec(`CREATE TABLE IF NOT EXISTS billing (user_id TEXT PRIMARY KEY REFERENCES users(id), customer TEXT UNIQUE,
		plan TEXT NOT NULL DEFAULT 'free', status TEXT NOT NULL DEFAULT '', subscription TEXT NOT NULL DEFAULT '',
		period_end INTEGER NOT NULL DEFAULT 0, cancel_at_period_end INTEGER NOT NULL DEFAULT 0, updated_at TEXT NOT NULL)`)
	if err != nil {
		return err
	}
	if _, err := s.db.Exec(`ALTER TABLE billing ADD COLUMN yearly INTEGER NOT NULL DEFAULT 0`); err != nil && !strings.Contains(err.Error(), "duplicate column") {
		return err
	}
	return nil
}

// Billing returns an account's subscription state; Plan is PlanFree when
// nothing is recorded.
func (s *Store) Billing(userID string) (Billing, error) {
	b := Billing{Plan: PlanFree}
	var customer sql.NullString
	var end int64
	err := s.db.QueryRow(`SELECT customer, plan, status, subscription, period_end, cancel_at_period_end, yearly FROM billing WHERE user_id = ?`, userID).
		Scan(&customer, &b.Plan, &b.Status, &b.Subscription, &end, &b.CancelAtPeriodEnd, &b.Yearly)
	if errors.Is(err, sql.ErrNoRows) {
		return b, nil
	}
	b.Customer = customer.String
	if end > 0 {
		b.PeriodEnd = time.Unix(end, 0)
	}
	return b, err
}

// Plan is the account's current plan.
func (s *Store) Plan(userID string) string {
	b, err := s.Billing(userID)
	if err != nil {
		return PlanFree
	}
	return b.Plan
}

// SetCustomer records the Stripe customer of an account.
func (s *Store) SetCustomer(userID, customer string) error {
	_, err := s.db.Exec(`INSERT INTO billing (user_id, customer, updated_at) VALUES (?, ?, ?)
		ON CONFLICT (user_id) DO UPDATE SET customer = excluded.customer, updated_at = excluded.updated_at`,
		userID, customer, s.now().UTC().Format(time.RFC3339))
	return err
}

// UserByCustomer finds the account of a Stripe customer.
func (s *Store) UserByCustomer(customer string) (string, error) {
	var id string
	if err := s.db.QueryRow(`SELECT user_id FROM billing WHERE customer = ?`, customer).Scan(&id); err != nil {
		return "", ErrNotFound
	}
	return id, nil
}

// StatusGranted marks a plan given by the operator, not paid through Stripe.
const StatusGranted = "granted"

// GrantPlan gives an account a plan without a subscription (the operator's
// own account, partners, trials). PlanFree removes a grant.
func (s *Store) GrantPlan(userID, plan string) error {
	if plan != PlanFree && plan != PlanPro && plan != PlanBusiness {
		return errors.New("plan must be free, pro or business")
	}
	status := StatusGranted
	if plan == PlanFree {
		status = ""
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`INSERT INTO billing (user_id, plan, status, updated_at) VALUES (?, ?, ?, ?)
		ON CONFLICT (user_id) DO UPDATE SET plan = excluded.plan, status = excluded.status, subscription = '', period_end = 0,
		cancel_at_period_end = 0, yearly = 0, updated_at = excluded.updated_at`, userID, plan, status, s.now().UTC().Format(time.RFC3339)); err != nil {
		return err
	}
	// A grant replaces any subscription: the account no longer pays.
	if err := s.endPaid(tx, userID); err != nil {
		return err
	}
	return tx.Commit()
}

// SetSubscription records the subscription state that decides the plan.
func (s *Store) SetSubscription(userID string, b Billing) error {
	var end int64
	if !b.PeriodEnd.IsZero() {
		end = b.PeriodEnd.Unix()
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.Exec(`UPDATE billing SET plan = ?, status = ?, subscription = ?, period_end = ?, cancel_at_period_end = ?, yearly = ?, updated_at = ?
		WHERE user_id = ?`, b.Plan, b.Status, b.Subscription, end, b.CancelAtPeriodEnd, b.Yearly, s.now().UTC().Format(time.RFC3339), userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n > 0 {
		// Paid periods feed churn and retention on the operator page.
		if b.Plan != PlanFree && b.Status != StatusGranted {
			err = s.recordPaid(tx, userID, b.Plan)
		} else {
			err = s.endPaid(tx, userID)
		}
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ChangePassword sets a new password after checking the current one. It
// signs out every other session and revokes agent tokens; the caller starts
// a fresh session for the browser in use.
func (s *Store) ChangePassword(userID, current, next string) error {
	var encoded string
	if err := s.db.QueryRow(`SELECT password_hash FROM users WHERE id = ?`, userID).Scan(&encoded); err != nil {
		return ErrNotFound
	}
	if !password.Check(encoded, current) {
		return ErrBadCredentials
	}
	if err := password.Validate(next); err != nil {
		return err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, q := range []struct {
		sql  string
		args []any
	}{
		{`UPDATE users SET password_hash = ? WHERE id = ?`, []any{password.Hash(next), userID}},
		{`DELETE FROM sessions WHERE user_id = ?`, []any{userID}},
		{`DELETE FROM tokens WHERE user_id = ?`, []any{userID}},
	} {
		if _, err := tx.Exec(q.sql, q.args...); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// DeleteUser removes an account and everything stored for it here, except
// its anonymous paid periods (see PaidPeriods).
func (s *Store) DeleteUser(userID string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, q := range []string{
		`DELETE FROM sessions WHERE user_id = ?`, `DELETE FROM tokens WHERE user_id = ?`, `DELETE FROM codes WHERE user_id = ?`, `DELETE FROM grants WHERE user_id = ?`,
		`DELETE FROM email_tokens WHERE user_id = ?`, `DELETE FROM billing WHERE user_id = ?`, `DELETE FROM users WHERE id = ?`,
	} {
		if _, err := tx.Exec(q, userID); err != nil {
			return err
		}
	}
	// The account's paid periods stay, closed: they hold no personal data
	// and a deleted paying account is churn.
	if err := s.endPaid(tx, userID); err != nil {
		return err
	}
	return tx.Commit()
}

// UserRow is one account as the Renoxium operator sees it.
type UserRow struct {
	ID, Email, CreatedAt, Plan, Status string
	Verified                           bool
}

// Users lists accounts for the operator, newest first, optionally filtered
// by email.
func (s *Store) Users(search string, limit int) ([]UserRow, error) {
	q := `SELECT u.id, u.email, u.created_at, u.verified_at IS NOT NULL, coalesce(b.plan, 'free'), coalesce(b.status, '')
		FROM users u LEFT JOIN billing b ON b.user_id = u.id`
	var args []any
	if search != "" {
		q += ` WHERE u.email LIKE ? ESCAPE '\'`
		args = append(args, "%"+strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(strings.ToLower(search))+"%")
	}
	q += ` ORDER BY u.created_at DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []UserRow{}
	for rows.Next() {
		var u UserRow
		if err := rows.Scan(&u.ID, &u.Email, &u.CreatedAt, &u.Verified, &u.Plan, &u.Status); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// PlanCounts counts accounts per plan, and how many of the paid ones pay
// through Stripe (not granted).
func (s *Store) PlanCounts() (total int, perPlan map[string]int, paying map[string]int, err error) {
	perPlan, paying = map[string]int{}, map[string]int{}
	if err = s.db.QueryRow(`SELECT count(*) FROM users`).Scan(&total); err != nil {
		return
	}
	rows, err := s.db.Query(`SELECT plan, status, count(*) FROM billing GROUP BY plan, status`)
	if err != nil {
		return
	}
	defer rows.Close()
	paid := 0
	for rows.Next() {
		var plan, status string
		var n int
		if err = rows.Scan(&plan, &status, &n); err != nil {
			return
		}
		if plan == PlanFree {
			continue
		}
		perPlan[plan] += n
		paid += n
		if status != StatusGranted {
			paying[plan] += n
		}
	}
	perPlan[PlanFree] = total - paid
	return total, perPlan, paying, rows.Err()
}
