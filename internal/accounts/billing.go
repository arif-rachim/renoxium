package accounts

import (
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/arif-rachim/renoxium/internal/password"
)

// Account plans. An account is a member while it has an unexpired card
// saved with Renoxium (saving one charges nothing), or while the operator
// has granted it membership; otherwise it is free. Apps are paid for one by
// one either way (internal/platform/appplan.go); only a grant waives that.
const (
	PlanFree   = "free"
	PlanMember = "member"
	// PlanGranted is what OwnerPlan reports for a member by agreement with
	// Renoxium: the member limits, and no shop fee or work app subscription.
	PlanGranted = "granted"
)

// Billing is an account's standing with Renoxium.
type Billing struct {
	Customer string // Stripe customer on the platform account, cus_...
	Plan     string // PlanFree or PlanMember, as it is now
	Status   string // StatusGranted for a grant, otherwise ""
	// The saved card, as last read from Stripe. CardExpires is the first
	// moment it no longer works (the start of the month after its expiry
	// month, UTC); zero when no card is saved.
	CardBrand, CardLast4 string
	CardExpires          time.Time
}

// HasCard reports whether a card is saved and not yet expired.
func (b Billing) HasCard(now time.Time) bool {
	return !b.CardExpires.IsZero() && now.Before(b.CardExpires)
}

// Granted reports whether the operator gave the account its membership.
func (b Billing) Granted() bool { return b.Status == StatusGranted && b.Plan != PlanFree }

// Card is a card payment method as Stripe has it.
type Card struct {
	Brand, Last4      string
	ExpMonth, ExpYear int
}

// Expires is the first moment the card no longer works.
func (c Card) Expires() time.Time {
	if c.ExpYear == 0 || c.ExpMonth < 1 || c.ExpMonth > 12 {
		return time.Time{}
	}
	return time.Date(c.ExpYear, time.Month(c.ExpMonth)+1, 1, 0, 0, 0, 0, time.UTC)
}

// planSQL is the account's plan in SQL, for a billing row b (or none) and
// the current time as unix seconds in the one argument it takes.
const planSQL = `CASE WHEN (coalesce(b.status, '') = 'granted' AND coalesce(b.plan, 'free') != 'free') OR coalesce(b.card_exp, 0) > ? THEN 'member' ELSE 'free' END`

func (s *Store) ensureBilling() error {
	if _, err := s.db.Exec(`CREATE TABLE IF NOT EXISTS billing (user_id TEXT PRIMARY KEY REFERENCES users(id), customer TEXT UNIQUE,
		plan TEXT NOT NULL DEFAULT 'free', status TEXT NOT NULL DEFAULT '', subscription TEXT NOT NULL DEFAULT '',
		period_end INTEGER NOT NULL DEFAULT 0, cancel_at_period_end INTEGER NOT NULL DEFAULT 0, updated_at TEXT NOT NULL)`); err != nil {
		return err
	}
	for _, col := range []string{`card_brand TEXT NOT NULL DEFAULT ''`, `card_last4 TEXT NOT NULL DEFAULT ''`, `card_exp INTEGER NOT NULL DEFAULT 0`} {
		if _, err := s.db.Exec(`ALTER TABLE billing ADD COLUMN ` + col); err != nil && !strings.Contains(err.Error(), "duplicate column") {
			return err
		}
	}
	// The Pro and Business plans are gone: their rows become members (a
	// grant stays a grant). The subscription columns are no longer used.
	_, err := s.db.Exec(`UPDATE billing SET plan = 'member' WHERE plan IN ('pro', 'business')`)
	return err
}

// Billing returns an account's standing; Plan is PlanFree when nothing is
// recorded.
func (s *Store) Billing(userID string) (Billing, error) {
	b := Billing{Plan: PlanFree}
	var customer sql.NullString
	var stored string
	var exp int64
	err := s.db.QueryRow(`SELECT customer, plan, status, card_brand, card_last4, card_exp FROM billing WHERE user_id = ?`, userID).
		Scan(&customer, &stored, &b.Status, &b.CardBrand, &b.CardLast4, &exp)
	if errors.Is(err, sql.ErrNoRows) {
		return b, nil
	}
	if err != nil {
		return b, err
	}
	b.Customer = customer.String
	if exp > 0 {
		b.CardExpires = time.Unix(exp, 0).UTC()
	}
	if (b.Status == StatusGranted && stored != PlanFree) || b.HasCard(s.now()) {
		b.Plan = PlanMember
	}
	return b, nil
}

// Plan is the account's current plan, PlanFree or PlanMember.
func (s *Store) Plan(userID string) string {
	b, err := s.Billing(userID)
	if err != nil {
		return PlanFree
	}
	return b.Plan
}

// OwnerPlan is the plan the platform applies to the account's apps:
// PlanFree, PlanMember, or PlanGranted for a member by agreement.
func (s *Store) OwnerPlan(userID string) string {
	b, err := s.Billing(userID)
	switch {
	case err != nil:
		return PlanFree
	case b.Granted():
		return PlanGranted
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

// SetCard records the card saved on the account's Stripe customer; a zero
// Card records that none is saved. A card that has expired counts as none.
func (s *Store) SetCard(userID string, c Card) error {
	var exp int64
	if t := c.Expires(); !t.IsZero() {
		exp = t.Unix()
	}
	if exp == 0 {
		c = Card{}
	}
	_, err := s.db.Exec(`INSERT INTO billing (user_id, card_brand, card_last4, card_exp, updated_at) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (user_id) DO UPDATE SET card_brand = excluded.card_brand, card_last4 = excluded.card_last4, card_exp = excluded.card_exp,
		updated_at = excluded.updated_at`, userID, c.Brand, c.Last4, exp, s.now().UTC().Format(time.RFC3339))
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

// StatusGranted marks a membership given by the operator, not by a card.
const StatusGranted = "granted"

// GrantPlan makes an account a member by agreement with Renoxium (the
// operator's own account, partners, reviewers, a self-hosted server's
// admin): member limits, and no shop fee or work app subscription for its
// apps. PlanFree removes a grant. The former plan names "pro" and
// "business", which older self-host licences carry, grant membership too.
func (s *Store) GrantPlan(userID, plan string) error {
	switch plan {
	case PlanFree, PlanMember:
	case "pro", "business":
		plan = PlanMember
	default:
		return errors.New("plan must be free or member")
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
		cancel_at_period_end = 0, updated_at = excluded.updated_at`, userID, plan, status, s.now().UTC().Format(time.RFC3339)); err != nil {
		return err
	}
	// A grant closes any paid period left from the former paid plans.
	if err := s.endPaid(tx, userID); err != nil {
		return err
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
	q := `SELECT u.id, u.email, u.created_at, u.verified_at IS NOT NULL, ` + planSQL + `, coalesce(b.status, '')
		FROM users u LEFT JOIN billing b ON b.user_id = u.id`
	args := []any{s.now().Unix()}
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

// PlanCounts counts accounts per plan (PlanFree, PlanMember), and how many
// of the members are members by the operator's grant.
func (s *Store) PlanCounts() (total int, perPlan map[string]int, granted int, err error) {
	perPlan = map[string]int{PlanFree: 0, PlanMember: 0}
	rows, err := s.db.Query(`SELECT `+planSQL+`, coalesce(b.status, '') = 'granted', count(*) FROM users u LEFT JOIN billing b ON b.user_id = u.id
		GROUP BY 1, 2`, s.now().Unix())
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var plan string
		var isGrant bool
		var n int
		if err = rows.Scan(&plan, &isGrant, &n); err != nil {
			return
		}
		total += n
		perPlan[plan] += n
		if plan == PlanMember && isGrant {
			granted += n
		}
	}
	return total, perPlan, granted, rows.Err()
}
