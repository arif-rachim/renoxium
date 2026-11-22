package accounts

import (
	"database/sql"
	"strings"
	"time"
)

// Plan prices in AED per month, shown on the billing page and used for
// revenue figures. Stripe charges what its price objects say; keep these
// in step with STRIPE_PRICE_PRO and STRIPE_PRICE_BUSINESS.
var PlanPriceAED = map[string]int{PlanPro: 99, PlanBusiness: 299}

// YearlyDiscountPercent is what paying for a year at once saves against
// twelve monthly payments.
const YearlyDiscountPercent = 20

// PlanPriceYearAED is a plan's price for a year paid at once, in whole AED;
// keep the yearly Stripe prices in step with it.
func PlanPriceYearAED(plan string) int {
	return PlanPriceAED[plan] * 12 * (100 - YearlyDiscountPercent) / 100
}

// ensureGrowth adds what the operator's funnel needs: when an account
// first connected an agent, and its paid periods (one row per continuous
// paid subscription, for churn and retention). Both are backfilled from
// what is already stored: agent tokens still held (their issue time is
// recovered from the refresh expiry, so it is the latest grant, not
// necessarily the first) and accounts paying now (start = last billing
// change).
func (s *Store) ensureGrowth() error {
	if _, err := s.db.Exec(`ALTER TABLE users ADD COLUMN agent_connected_at TEXT`); err != nil && !strings.Contains(err.Error(), "duplicate column") {
		return err
	}
	for _, stmt := range []string{
		`CREATE TABLE IF NOT EXISTS paid_periods (user_id TEXT NOT NULL, plan TEXT NOT NULL, started_at TEXT NOT NULL, ended_at TEXT)`,
		`CREATE INDEX IF NOT EXISTS paid_periods_user ON paid_periods (user_id)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS paid_periods_open ON paid_periods (user_id) WHERE ended_at IS NULL`,
	} {
		if _, err := s.db.Exec(stmt); err != nil {
			return err
		}
	}
	if _, err := s.db.Exec(`UPDATE users SET agent_connected_at = (SELECT strftime('%Y-%m-%dT%H:%M:%SZ', min(refresh_expires_at) - ?, 'unixepoch')
		FROM tokens WHERE tokens.user_id = users.id) WHERE agent_connected_at IS NULL AND EXISTS (SELECT 1 FROM tokens WHERE tokens.user_id = users.id)`,
		int64(RefreshTokenTTL.Seconds())); err != nil {
		return err
	}
	_, err := s.db.Exec(`INSERT INTO paid_periods (user_id, plan, started_at) SELECT user_id, plan, updated_at FROM billing b
		WHERE plan != 'free' AND status != ? AND NOT EXISTS (SELECT 1 FROM paid_periods p WHERE p.user_id = b.user_id)`, StatusGranted)
	return err
}

// recordAgent notes the first time an account's agent got tokens.
func (s *Store) recordAgent(tx *sql.Tx, userID string) error {
	_, err := tx.Exec(`UPDATE users SET agent_connected_at = ? WHERE id = ? AND agent_connected_at IS NULL`, s.now().UTC().Format(time.RFC3339), userID)
	return err
}

// recordPaid opens a paid period, or keeps the open one with the new plan.
func (s *Store) recordPaid(tx *sql.Tx, userID, plan string) error {
	_, err := tx.Exec(`INSERT INTO paid_periods (user_id, plan, started_at) VALUES (?, ?, ?)
		ON CONFLICT (user_id) WHERE ended_at IS NULL DO UPDATE SET plan = excluded.plan`, userID, plan, s.now().UTC().Format(time.RFC3339))
	return err
}

// endPaid closes the open paid period, if any.
func (s *Store) endPaid(tx *sql.Tx, userID string) error {
	_, err := tx.Exec(`UPDATE paid_periods SET ended_at = ? WHERE user_id = ? AND ended_at IS NULL`, s.now().UTC().Format(time.RFC3339), userID)
	return err
}

// GrowthAccount is one account's funnel timestamps; zero when not reached.
type GrowthAccount struct {
	ID, Email, Plan, Status      string
	CreatedAt, VerifiedAt, Agent time.Time
}

// GrowthAccounts lists every account with its funnel timestamps.
func (s *Store) GrowthAccounts() ([]GrowthAccount, error) {
	rows, err := s.db.Query(`SELECT u.id, u.email, u.created_at, coalesce(u.verified_at, ''), coalesce(u.agent_connected_at, ''),
		coalesce(b.plan, 'free'), coalesce(b.status, '') FROM users u LEFT JOIN billing b ON b.user_id = u.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []GrowthAccount
	for rows.Next() {
		var a GrowthAccount
		var created, verified, agent string
		if err := rows.Scan(&a.ID, &a.Email, &created, &verified, &agent, &a.Plan, &a.Status); err != nil {
			return nil, err
		}
		a.CreatedAt, a.VerifiedAt, a.Agent = parseTime(created), parseTime(verified), parseTime(agent)
		out = append(out, a)
	}
	return out, rows.Err()
}

// PaidPeriod is one continuous paid subscription. End is zero while open.
type PaidPeriod struct {
	UserID, Plan string
	Start, End   time.Time
}

// PaidPeriods lists every paid period, deleted accounts' included (they
// count as churn).
func (s *Store) PaidPeriods() ([]PaidPeriod, error) {
	rows, err := s.db.Query(`SELECT user_id, plan, started_at, coalesce(ended_at, '') FROM paid_periods`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PaidPeriod
	for rows.Next() {
		var p PaidPeriod
		var start, end string
		if err := rows.Scan(&p.UserID, &p.Plan, &start, &end); err != nil {
			return nil, err
		}
		p.Start, p.End = parseTime(start), parseTime(end)
		out = append(out, p)
	}
	return out, rows.Err()
}

func parseTime(v string) time.Time {
	if v == "" {
		return time.Time{}
	}
	for _, layout := range []string{time.RFC3339, time.DateTime} {
		if t, err := time.Parse(layout, v); err == nil {
			return t.UTC()
		}
	}
	return time.Time{}
}
