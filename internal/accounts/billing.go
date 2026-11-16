package accounts

import (
	"database/sql"
	"errors"
	"time"
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
}

func (s *Store) ensureBilling() error {
	_, err := s.db.Exec(`CREATE TABLE IF NOT EXISTS billing (user_id TEXT PRIMARY KEY REFERENCES users(id), customer TEXT UNIQUE,
		plan TEXT NOT NULL DEFAULT 'free', status TEXT NOT NULL DEFAULT '', subscription TEXT NOT NULL DEFAULT '',
		period_end INTEGER NOT NULL DEFAULT 0, cancel_at_period_end INTEGER NOT NULL DEFAULT 0, updated_at TEXT NOT NULL)`)
	return err
}

// Billing returns an account's subscription state; Plan is PlanFree when
// nothing is recorded.
func (s *Store) Billing(userID string) (Billing, error) {
	b := Billing{Plan: PlanFree}
	var customer sql.NullString
	var end int64
	err := s.db.QueryRow(`SELECT customer, plan, status, subscription, period_end, cancel_at_period_end FROM billing WHERE user_id = ?`, userID).
		Scan(&customer, &b.Plan, &b.Status, &b.Subscription, &end, &b.CancelAtPeriodEnd)
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
	_, err := s.db.Exec(`INSERT INTO billing (user_id, plan, status, updated_at) VALUES (?, ?, ?, ?)
		ON CONFLICT (user_id) DO UPDATE SET plan = excluded.plan, status = excluded.status, subscription = '', period_end = 0,
		cancel_at_period_end = 0, updated_at = excluded.updated_at`, userID, plan, status, s.now().UTC().Format(time.RFC3339))
	return err
}

// SetSubscription records the subscription state that decides the plan.
func (s *Store) SetSubscription(userID string, b Billing) error {
	var end int64
	if !b.PeriodEnd.IsZero() {
		end = b.PeriodEnd.Unix()
	}
	_, err := s.db.Exec(`UPDATE billing SET plan = ?, status = ?, subscription = ?, period_end = ?, cancel_at_period_end = ?, updated_at = ?
		WHERE user_id = ?`, b.Plan, b.Status, b.Subscription, end, b.CancelAtPeriodEnd, s.now().UTC().Format(time.RFC3339), userID)
	return err
}
