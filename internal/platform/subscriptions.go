package platform

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"strings"
	"time"

	"github.com/arif-rachim/renoxium/internal/stripe"
)

// Recurring payments: payments.subscribe starts a Stripe Checkout Session
// in subscription mode on the merchant's account (inline recurring prices,
// so each invoice is a direct charge there). Each invoice becomes a row in
// rx_payments and runs on_payment: "paid" for the first, "renewed" for each
// later one, "renewal_failed" when a charge fails, "subscription_cancelled"
// when it ends. Nothing charges saved cards from scripts.

// subscriptionPrefix starts Renoxium's own subscription ids, which also
// address the simulated checkout of previews.
const subscriptionPrefix = "rxs_"

// maxIntervalCount is Stripe's limit: at most three years between bills.
var maxIntervalCount = map[string]int64{"week": 156, "month": 36, "year": 3}

// Subscription is a recurring payment as the owner and scripts see it.
type Subscription struct {
	ID                string  `json:"id"`
	Reference         string  `json:"reference"`
	Status            string  `json:"status"` // pending, trialing, active, past_due, canceled, expired, ...
	Amount            float64 `json:"amount"` // per period
	Currency          string  `json:"currency"`
	Interval          string  `json:"interval"`
	IntervalCount     int64   `json:"interval_count"`
	TrialDays         int64   `json:"trial_days,omitempty"`
	EndAfter          int64   `json:"end_after,omitempty"`
	PaymentsMade      int64   `json:"payments_made"`
	CurrentPeriodEnd  string  `json:"current_period_end"` // UTC "YYYY-MM-DD HH:MM:SS": the next payment date
	CancelAtPeriodEnd bool    `json:"cancel_at_period_end"`
	CustomerEmail     string  `json:"customer_email"`
	CustomerName      string  `json:"customer_name"`
	CreatedAt         string  `json:"created_at"`
	UpdatedAt         string  `json:"updated_at,omitempty"`

	minor                   int64
	stripeID, items         string
	successPath, cancelPath string
	feePercent              float64
}

func querySubscriptions(ctx context.Context, st *stage, tail string, args ...any) ([]Subscription, error) {
	rows, err := st.admin.QueryContext(ctx, `SELECT id, reference, status, amount, currency, interval, interval_count, trial_days, end_after,
		paid_count, current_period_end, cancel_at_period_end, customer_email, customer_name, created_at, coalesce(updated_at, ''),
		stripe_id, items, success_path, cancel_path, fee_percent FROM rx_subscriptions `+tail, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Subscription{}
	for rows.Next() {
		var s Subscription
		if err := rows.Scan(&s.ID, &s.Reference, &s.Status, &s.minor, &s.Currency, &s.Interval, &s.IntervalCount, &s.TrialDays, &s.EndAfter,
			&s.PaymentsMade, &s.CurrentPeriodEnd, &s.CancelAtPeriodEnd, &s.CustomerEmail, &s.CustomerName, &s.CreatedAt, &s.UpdatedAt,
			&s.stripeID, &s.items, &s.successPath, &s.cancelPath, &s.feePercent); err != nil {
			return nil, err
		}
		s.Amount = float64(s.minor) / 100
		out = append(out, s)
	}
	return out, rows.Err()
}

func subscriptionByID(ctx context.Context, st *stage, id string) (Subscription, error) {
	list, err := querySubscriptions(ctx, st, `WHERE id = ?`, id)
	if err != nil {
		return Subscription{}, err
	}
	if len(list) == 0 {
		return Subscription{}, fmt.Errorf("subscription %q: %w", id, ErrNotFound)
	}
	return list[0], nil
}

// Subscriptions lists an app's newest live subscriptions.
func (p *Platform) Subscriptions(ctx context.Context, app string, limit int) ([]Subscription, error) {
	st, err := p.loadStage(app, Live)
	if err != nil || st == nil {
		return nil, err
	}
	return querySubscriptions(ctx, st, `WHERE status != 'pending' AND status != 'expired' ORDER BY created_at DESC, rowid DESC LIMIT ?`, limit)
}

// feePercent is Renoxium's share of subscription invoices: a shop's fee, as
// for one-off payments.
func (p *Platform) feePercent(app string) float64 {
	if p.applicationFee(app, 10000) == 0 {
		return 0
	}
	return FreePlanFeePercent
}

type subscribeInput struct {
	checkoutInput
	Interval      string `json:"interval"`
	IntervalCount int64  `json:"interval_count"`
	TrialDays     int64  `json:"trial_days"`
	EndAfter      int64  `json:"end_after"`
}

// subscribe answers payments.subscribe.
func (p *Platform) subscribe(ctx context.Context, st *stage, arg any) (any, error) {
	var in subscribeInput
	if err := decodeArg(arg, &in); err != nil {
		return nil, errors.New(`payments.subscribe expects {items: [{name, price, quantity}], interval: "week"|"month"|"year", reference, success_path, cancel_path}`)
	}
	if in.Hold {
		return nil, errors.New("hold is for payments.checkout only")
	}
	items, total, err := in.validate()
	if err != nil {
		return nil, err
	}
	in.Interval = strings.ToLower(strings.TrimSpace(in.Interval))
	limit, ok := maxIntervalCount[in.Interval]
	switch {
	case !ok:
		return nil, errors.New(`interval must be "week", "month" or "year"`)
	case in.IntervalCount == 0:
		in.IntervalCount = 1
	}
	switch {
	case in.IntervalCount < 1 || in.IntervalCount > limit:
		return nil, fmt.Errorf("interval_count must be between 1 and %d for %s", limit, in.Interval)
	case in.TrialDays < 0 || in.TrialDays > 730:
		return nil, errors.New("trial_days must be between 0 and 730")
	case in.EndAfter < 0 || in.EndAfter > 1000:
		return nil, errors.New("end_after is the number of payments before the subscription ends (0 = until cancelled, at most 1000)")
	}
	id := subscriptionPrefix + secretToken()[:24]
	itemsJSON, _ := json.Marshal(in.Items)
	fee := 0.0
	url := PayPrefix + id
	if st.name != Preview {
		if p.cfg.Stripe == nil {
			return nil, errors.New("payments are not set up on this Renoxium server")
		}
		pa, err := p.PaymentAccount(st.app)
		if err != nil {
			return nil, err
		}
		if pa.StripeAccount == "" || !pa.ChargesEnabled {
			return nil, errors.New("the owner has not finished connecting Stripe yet (Renoxium admin panel, Payments)")
		}
		fee = p.feePercent(st.app)
		base := p.URL(st.app, Live)
		s, err := p.cfg.Stripe.CreateSubscriptionCheckout(ctx, pa.StripeAccount, stripe.SubscriptionParams{
			Currency: in.Currency, Items: items, Interval: in.Interval, IntervalCount: in.IntervalCount,
			SuccessURL: base + in.SuccessPath, CancelURL: base + in.CancelPath, CustomerEmail: in.CustomerEmail, Reference: in.Reference,
			Metadata:  map[string]string{"rx_app": st.app, "rx_reference": in.Reference, "rx_subscription": id},
			TrialDays: in.TrialDays, FeePercent: fee,
		})
		if err != nil {
			p.event(st, "payment", "error", "", "subscription checkout not created: "+err.Error())
			return nil, err
		}
		url = s.URL
	}
	_, err = st.admin.ExecContext(ctx, `INSERT INTO rx_subscriptions (id, reference, status, amount, currency, items, interval, interval_count,
		trial_days, end_after, fee_percent, customer_email, success_path, cancel_path) VALUES (?, ?, 'pending', ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, in.Reference, total, in.Currency, string(itemsJSON), in.Interval, in.IntervalCount, in.TrialDays, in.EndAfter, fee,
		in.CustomerEmail, in.SuccessPath, in.CancelPath)
	if err != nil {
		return nil, err
	}
	p.event(st, "payment", "info", "", fmt.Sprintf("subscription %s started for %s: %.2f %s every %s", id, in.Reference, float64(total)/100,
		strings.ToUpper(in.Currency), every(in.Interval, in.IntervalCount)))
	return map[string]any{"url": url, "id": id, "reference": in.Reference, "amount": float64(total) / 100, "currency": in.Currency,
		"interval": in.Interval, "interval_count": in.IntervalCount}, nil
}

func every(interval string, n int64) string {
	if n <= 1 {
		return interval
	}
	return fmt.Sprintf("%d %ss", n, interval)
}

// subscriptionStatus answers payments.subscription(reference): the newest
// subscription for the reference, one the customer started before ones
// still waiting at checkout.
func (p *Platform) subscriptionStatus(ctx context.Context, st *stage, arg any) (any, error) {
	ref, ok := arg.(string)
	if !ok || ref == "" {
		return nil, errors.New("payments.subscription expects the reference used in payments.subscribe")
	}
	list, err := querySubscriptions(ctx, st, `WHERE reference = ? ORDER BY CASE WHEN status IN ('pending', 'expired') THEN 1 ELSE 0 END,
		created_at DESC, rowid DESC LIMIT 1`, ref)
	if err != nil || len(list) == 0 {
		return nil, err
	}
	return list[0], nil
}

type cancelInput struct {
	Reference   string `json:"reference"`
	AtPeriodEnd bool   `json:"at_period_end"`
}

// scriptCancelSubscription answers payments.cancel_subscription.
func (p *Platform) scriptCancelSubscription(ctx context.Context, st *stage, arg any) (any, error) {
	var in cancelInput
	if err := decodeArg(arg, &in); err != nil || in.Reference == "" {
		return nil, errors.New("payments.cancel_subscription expects {reference, at_period_end}")
	}
	list, err := querySubscriptions(ctx, st, `WHERE reference = ? AND status NOT IN ('pending', 'expired', 'canceled') ORDER BY created_at DESC, rowid DESC LIMIT 1`, in.Reference)
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, fmt.Errorf("no running subscription for reference %q", in.Reference)
	}
	if err := p.allowMoney(st); err != nil {
		return nil, err
	}
	return p.cancelSubscription(ctx, st, list[0].ID, in.AtPeriodEnd, "a script")
}

// cancelSubscription ends a subscription now (on_payment gets
// "subscription_cancelled") or at the end of the paid period (on_payment
// gets it then).
func (p *Platform) cancelSubscription(ctx context.Context, st *stage, id string, atPeriodEnd bool, by string) (Subscription, error) {
	defer lockMoney(st, id)()
	sub, err := subscriptionByID(ctx, st, id)
	if err != nil {
		return sub, err
	}
	switch sub.Status {
	case "canceled":
		return sub, nil
	case "pending", "expired":
		return sub, fmt.Errorf("the customer has not subscribed for %s", sub.Reference)
	}
	if st.name == Live {
		account, err := p.connected(st)
		if err != nil {
			return sub, err
		}
		if sub.stripeID == "" {
			return sub, fmt.Errorf("the subscription for %s is not confirmed by Stripe yet", sub.Reference)
		}
		if atPeriodEnd {
			s, err := p.cfg.Stripe.SetCancelAtPeriodEnd(ctx, account, sub.stripeID, true)
			if err != nil {
				p.event(st, "payment", "error", "", fmt.Sprintf("cancelling the subscription for %s failed: %v", sub.Reference, err))
				return sub, err
			}
			p.applyStripeSubscription(ctx, st, sub.ID, s)
		} else if _, err := p.cfg.Stripe.CancelConnectedSubscription(ctx, account, sub.stripeID); err != nil {
			p.event(st, "payment", "error", "", fmt.Sprintf("cancelling the subscription for %s failed: %v", sub.Reference, err))
			return sub, err
		}
	}
	if atPeriodEnd {
		st.admin.ExecContext(ctx, `UPDATE rx_subscriptions SET cancel_at_period_end = 1, updated_at = datetime('now') WHERE id = ?`, sub.ID)
		p.event(st, "payment", "info", "", fmt.Sprintf("%s set the subscription for %s to end on %s", by, sub.Reference, orDefault(sub.CurrentPeriodEnd, "its period end")))
	} else {
		p.event(st, "payment", "warn", "", fmt.Sprintf("%s cancelled the subscription for %s", by, sub.Reference))
		if err := p.subscriptionEnded(ctx, st, sub.ID); err != nil {
			return sub, err
		}
	}
	return subscriptionByID(ctx, st, sub.ID)
}

// subscriptionEnded marks a subscription cancelled and runs on_payment
// once with "subscription_cancelled".
func (p *Platform) subscriptionEnded(ctx context.Context, st *stage, id string) error {
	res, err := st.admin.ExecContext(ctx, `UPDATE rx_subscriptions SET status = 'canceled', updated_at = datetime('now') WHERE id = ? AND status != 'canceled'`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil
	}
	sub, err := subscriptionByID(ctx, st, id)
	if err != nil {
		return err
	}
	p.event(st, "payment", "info", "", fmt.Sprintf("subscription for %s ended", sub.Reference))
	p.runHook(ctx, st, subscriptionHookBody(sub, "subscription_cancelled"))
	return nil
}

func subscriptionInfo(sub Subscription) map[string]any {
	return map[string]any{"id": sub.ID, "status": sub.Status, "interval": sub.Interval, "interval_count": sub.IntervalCount,
		"current_period_end": sub.CurrentPeriodEnd, "cancel_at_period_end": sub.CancelAtPeriodEnd, "payments_made": sub.PaymentsMade}
}

func subscriptionHookBody(sub Subscription, event string) map[string]any {
	return map[string]any{"event": event, "id": sub.ID, "reference": sub.Reference, "status": sub.Status, "amount": sub.Amount,
		"refunded_amount": 0, "currency": sub.Currency, "customer_email": sub.CustomerEmail, "customer_name": sub.CustomerName,
		"subscription": subscriptionInfo(sub), "period": map[string]any{"start": "", "end": sub.CurrentPeriodEnd}}
}

// invoice is one subscription payment, from Stripe or simulated.
type invoice struct {
	id            string
	paid          bool
	first         bool
	minor         int64
	start, end    time.Time
	email, name   string
	paymentIntent string
}

const dbTime = "2006-01-02 15:04:05"

// dbTimeOf formats t for the database, "" when unknown.
func dbTimeOf(t time.Time) string {
	if t.IsZero() || t.Unix() <= 0 {
		return ""
	}
	return t.UTC().Format(dbTime)
}

// recordInvoice stores a subscription payment and, when its status
// changes, runs on_payment ("paid" for the first, "renewed", or
// "renewal_failed"). A failed invoice that is paid on a retry runs it again.
func (p *Platform) recordInvoice(ctx context.Context, st *stage, sub Subscription, in invoice) error {
	defer lockMoney(st, sub.ID)()
	fee := int64(float64(in.minor)*sub.feePercent/100 + 0.5)
	_, err := st.admin.ExecContext(ctx, `INSERT OR IGNORE INTO rx_payments (id, reference, status, amount, currency, items, success_path, cancel_path,
		customer_email, customer_name, subscription, period_start, period_end, application_fee)
		VALUES (?, ?, 'pending', ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, in.id, sub.Reference, in.minor, sub.Currency, sub.items, sub.successPath, sub.cancelPath,
		orDefault(in.email, sub.CustomerEmail), orDefault(in.name, sub.CustomerName), sub.ID, dbTimeOf(in.start), dbTimeOf(in.end), fee)
	if err != nil {
		return err
	}
	status, from := "failed", "'pending'"
	if in.paid {
		status, from = "paid", "'pending', 'failed'"
	}
	res, err := st.admin.ExecContext(ctx, `UPDATE rx_payments SET status = ?, amount = ?, application_fee = ?,
		payment_intent = CASE WHEN ? != '' THEN ? ELSE payment_intent END, updated_at = datetime('now') WHERE id = ? AND status IN (`+from+`)`,
		status, in.minor, fee, in.paymentIntent, in.paymentIntent, in.id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil // already recorded
	}
	event := "renewal_failed"
	if in.paid {
		event = "renewed"
		if in.first {
			event = "paid"
		}
		newStatus := "active"
		if in.minor == 0 && in.first && sub.TrialDays > 0 {
			newStatus = "trialing"
		}
		count := int64(0)
		if in.minor > 0 {
			count = 1
		}
		st.admin.ExecContext(ctx, `UPDATE rx_subscriptions SET paid_count = paid_count + ?, current_period_end = CASE WHEN ? != '' THEN ? ELSE current_period_end END,
			status = CASE WHEN status = 'canceled' THEN status ELSE ? END, updated_at = datetime('now') WHERE id = ?`,
			count, dbTimeOf(in.end), dbTimeOf(in.end), newStatus, sub.ID)
	} else {
		st.admin.ExecContext(ctx, `UPDATE rx_subscriptions SET status = CASE WHEN status = 'canceled' THEN status ELSE 'past_due' END,
			updated_at = datetime('now') WHERE id = ?`, sub.ID)
	}
	if sub, err = subscriptionByID(ctx, st, sub.ID); err != nil {
		return err
	}
	pm, err := p.paymentByID(ctx, st, in.id)
	if err != nil {
		return err
	}
	p.event(st, "payment", "info", "", fmt.Sprintf("subscription payment %s for %s: %s", in.id, sub.Reference, event))
	if in.paid && in.minor > 0 {
		subject := fmt.Sprintf("Payment received: %s %.2f for %s (subscription)", strings.ToUpper(pm.Currency), pm.Amount, pm.Reference)
		p.whatsappOwner(st, subject)
		p.mailOwner(ctx, st, subject, fmt.Sprintf("A subscription payment was completed.\n\nReference: %s\nAmount: %s %.2f\nPeriod: %s to %s\nCustomer: %s %s\n\nThe money is in your Stripe account.",
			pm.Reference, strings.ToUpper(pm.Currency), pm.Amount, pm.PeriodStart, pm.PeriodEnd, pm.CustomerName, pm.CustomerEmail))
	}
	if !in.paid {
		p.mailOwner(ctx, st, fmt.Sprintf("Subscription payment failed for %s", sub.Reference),
			fmt.Sprintf("Stripe could not charge the card for the subscription %s (%s %.2f). Stripe retries automatically; the customer can update their card from Stripe's emails.",
				sub.Reference, strings.ToUpper(sub.Currency), sub.Amount))
	}
	body := paymentHookBody(pm, event)
	body["subscription"] = subscriptionInfo(sub)
	p.runHook(ctx, st, body)
	// end_after: stop after that many payments, at the end of the period paid.
	if in.paid && in.minor > 0 && sub.EndAfter > 0 && sub.PaymentsMade >= sub.EndAfter && !sub.CancelAtPeriodEnd && sub.Status != "canceled" {
		if st.name == Live && sub.stripeID != "" {
			if account, err := p.connected(st); err == nil {
				if _, err := p.cfg.Stripe.SetCancelAtPeriodEnd(ctx, account, sub.stripeID, true); err != nil {
					p.event(st, "payment", "error", "", fmt.Sprintf("could not end the subscription for %s after %d payments: %v", sub.Reference, sub.EndAfter, err))
					return nil
				}
			}
		}
		st.admin.ExecContext(ctx, `UPDATE rx_subscriptions SET cancel_at_period_end = 1 WHERE id = ?`, sub.ID)
		p.event(st, "payment", "info", "", fmt.Sprintf("subscription for %s ends after its %d payments, on %s", sub.Reference, sub.EndAfter, sub.CurrentPeriodEnd))
	}
	return nil
}

// --- Webhooks ---

// subscriptionCheckoutEvent links a completed subscription checkout to its
// Stripe subscription, or marks an abandoned one expired.
func (p *Platform) subscriptionCheckoutEvent(ctx context.Context, app, account, typ string, s stripe.Session) error {
	id := s.Metadata["rx_subscription"]
	if id == "" {
		return nil
	}
	st, err := p.loadStage(app, Live)
	if err != nil || st == nil {
		return err
	}
	switch typ {
	case "checkout.session.expired":
		_, err := st.admin.ExecContext(ctx, `UPDATE rx_subscriptions SET status = 'expired', updated_at = datetime('now') WHERE id = ? AND status = 'pending'`, id)
		return err
	case "checkout.session.completed":
	default:
		return nil
	}
	_, err = st.admin.ExecContext(ctx, `UPDATE rx_subscriptions SET stripe_id = CASE WHEN ? != '' THEN ? ELSE stripe_id END,
		customer_email = CASE WHEN ? != '' THEN ? ELSE customer_email END, customer_name = CASE WHEN ? != '' THEN ? ELSE customer_name END,
		status = CASE WHEN status = 'pending' THEN 'active' ELSE status END, updated_at = datetime('now') WHERE id = ?`,
		s.Subscription, s.Subscription, s.Customer.Email, s.Customer.Email, s.Customer.Name, s.Customer.Name, id)
	if err != nil || s.Subscription == "" {
		return err
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
	defer cancel()
	if sub, err := p.cfg.Stripe.GetConnectedSubscription(ctx, account, s.Subscription); err == nil {
		p.applyStripeSubscription(ctx, st, id, sub)
	}
	return nil
}

// applyStripeSubscription copies Stripe's status, period end and
// cancel_at_period_end, unless the subscription already ended here.
func (p *Platform) applyStripeSubscription(ctx context.Context, st *stage, id string, s stripe.Subscription) {
	if s.Status == "canceled" {
		s.Status = "" // set by subscriptionEnded, which runs on_payment once
	}
	end := ""
	if t := s.PeriodEnd(); !t.IsZero() {
		end = t.UTC().Format(dbTime)
	}
	st.admin.ExecContext(ctx, `UPDATE rx_subscriptions SET status = CASE WHEN ? != '' THEN ? ELSE status END,
		current_period_end = CASE WHEN ? != '' THEN ? ELSE current_period_end END, cancel_at_period_end = ?,
		stripe_id = CASE WHEN stripe_id = '' THEN ? ELSE stripe_id END, updated_at = datetime('now') WHERE id = ? AND status != 'canceled'`,
		s.Status, s.Status, end, end, s.CancelAtPeriodEnd, s.ID, id)
}

// subscriptionOf finds the Renoxium subscription for Stripe metadata or a
// Stripe subscription id.
func (p *Platform) subscriptionOf(ctx context.Context, st *stage, meta map[string]string, stripeID string) (Subscription, bool) {
	if id := meta["rx_subscription"]; id != "" {
		if meta["rx_app"] != "" && p.currentName(meta["rx_app"]) != st.app { // renamed apps keep their subscriptions
			return Subscription{}, false
		}
		if sub, err := subscriptionByID(ctx, st, id); err == nil {
			return sub, true
		}
	}
	if stripeID != "" {
		if list, err := querySubscriptions(ctx, st, `WHERE stripe_id = ?`, stripeID); err == nil && len(list) == 1 {
			return list[0], true
		}
	}
	return Subscription{}, false
}

// invoiceEvent applies invoice.paid and invoice.payment_failed.
func (p *Platform) invoiceEvent(ctx context.Context, st *stage, account string, paid bool, in stripe.Invoice) error {
	sub, ok := p.subscriptionOf(ctx, st, in.SubscriptionMetadata(), in.SubscriptionID())
	if !ok {
		return nil // not a Renoxium subscription
	}
	if sub.stripeID == "" && in.SubscriptionID() != "" {
		st.admin.ExecContext(ctx, `UPDATE rx_subscriptions SET stripe_id = ? WHERE id = ? AND stripe_id = ''`, in.SubscriptionID(), sub.ID)
		sub.stripeID = in.SubscriptionID()
	}
	start, end := in.ServicePeriod()
	minor := in.AmountPaid
	if !paid {
		minor = in.AmountDue
	}
	pi := in.LegacyPaymentIntent
	if paid && pi == "" && minor > 0 && p.cfg.Stripe != nil {
		pi, _ = p.cfg.Stripe.InvoicePaymentIntent(ctx, account, in.ID) // for refunds; found again at refund time if this fails
	}
	return p.recordInvoice(ctx, st, sub, invoice{id: in.ID, paid: paid, first: in.BillingReason == "subscription_create", minor: minor,
		start: start, end: end, email: in.CustomerEmail, name: in.CustomerName, paymentIntent: pi})
}

// subscriptionEvent applies customer.subscription.* events.
func (p *Platform) subscriptionEvent(ctx context.Context, st *stage, s stripe.Subscription, deleted bool) error {
	sub, ok := p.subscriptionOf(ctx, st, s.Metadata, s.ID)
	if !ok {
		return nil
	}
	p.applyStripeSubscription(ctx, st, sub.ID, s)
	if deleted || s.Status == "canceled" {
		return p.subscriptionEnded(ctx, st, sub.ID)
	}
	return nil
}

// --- Preview simulation ---

func addInterval(t time.Time, interval string, n int64) time.Time {
	switch interval {
	case "week":
		return t.AddDate(0, 0, 7*int(n))
	case "year":
		return t.AddDate(int(n), 0, 0)
	}
	return t.AddDate(0, int(n), 0)
}

// serveSubscriptionPay is the simulated subscription checkout of previews.
// decision "pay" subscribes (the first payment, or a trial); afterwards
// "renew" and "renewal_failed" simulate the next invoice.
func (p *Platform) serveSubscriptionPay(w http.ResponseWriter, r *http.Request, st *stage, id string) {
	sub, err := subscriptionByID(r.Context(), st, id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	ctx := r.Context()
	if r.Method == http.MethodPost {
		r.Body = http.MaxBytesReader(w, r.Body, 8<<10)
		if !p.validCSRF(r) {
			http.Error(w, "the form expired; reload the page", http.StatusForbidden)
			return
		}
		now := time.Now().UTC()
		switch d := r.PostFormValue("decision"); {
		case d == "pay" && sub.Status == "pending":
			start, end, minor := now, addInterval(now, sub.Interval, sub.IntervalCount), sub.minor
			if sub.TrialDays > 0 {
				end, minor = now.AddDate(0, 0, int(sub.TrialDays)), 0
			}
			st.admin.ExecContext(ctx, `UPDATE rx_subscriptions SET customer_email = CASE WHEN customer_email = '' THEN 'preview.buyer@example.com' ELSE customer_email END,
				customer_name = 'Preview buyer' WHERE id = ?`, id)
			sub, _ = subscriptionByID(ctx, st, id)
			if err := p.recordInvoice(ctx, st, sub, invoice{id: "sim_in_" + secretToken()[:20], paid: true, first: true, minor: minor, start: start, end: end}); err != nil {
				http.Error(w, "could not record the payment", http.StatusInternalServerError)
				return
			}
			http.Redirect(w, r, sub.successPath, http.StatusSeeOther)
		case (d == "renew" || d == "renewal_failed") && sub.Status != "pending" && sub.Status != "canceled" && sub.Status != "expired":
			if d == "renew" && sub.CancelAtPeriodEnd {
				p.subscriptionEnded(ctx, st, id) // the period ended
			} else {
				start, _ := time.Parse(dbTime, sub.CurrentPeriodEnd)
				if start.IsZero() {
					start = now
				}
				if err := p.recordInvoice(ctx, st, sub, invoice{id: "sim_in_" + secretToken()[:20], paid: d == "renew", minor: sub.minor,
					start: start, end: addInterval(start, sub.Interval, sub.IntervalCount)}); err != nil {
					http.Error(w, "could not record the payment", http.StatusInternalServerError)
					return
				}
			}
			http.Redirect(w, r, PayPrefix+id, http.StatusSeeOther)
		case d == "pay" || d == "renew" || d == "renewal_failed":
			http.Error(w, "the subscription is "+sub.Status, http.StatusConflict)
		default:
			http.Redirect(w, r, sub.cancelPath, http.StatusSeeOther)
		}
		return
	}
	var items []checkoutItem
	json.Unmarshal([]byte(sub.items), &items)
	data := map[string]any{"csrf": p.csrf(w, r), "sub": sub, "items": items, "every": every(sub.Interval, sub.IntervalCount),
		"total": fmt.Sprintf("%.2f %s", sub.Amount, strings.ToUpper(sub.Currency))}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	simulatedSubscription.Execute(w, data)
}

var simulatedSubscription = template.Must(template.New("subscribe").Parse(`<!doctype html><html lang="en"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1"><title>Simulated subscription</title>
<style>:root{color-scheme:light dark;--fg:#111827;--muted:#5b6474;--bg:#f4f5f7;--card:#fff;--line:#dcdfe5;--accent:#13203a;--on:#fff}
@media (prefers-color-scheme:dark){:root{--fg:#eef0f4;--muted:#a3abb9;--bg:#0f1218;--card:#181c24;--line:#2c3240;--accent:#e7ecf4;--on:#0f1218}}
*{box-sizing:border-box}body{margin:0;background:var(--bg);color:var(--fg);font:16px/1.55 system-ui,-apple-system,"Segoe UI",Roboto,sans-serif;padding:24px 16px}
body>*{max-width:460px;margin-left:auto;margin-right:auto}h1{font-size:26px;margin:20px auto 6px;letter-spacing:-.01em}body>p{color:var(--muted)}
.note{background:#fff3cd;color:#664d03;border-radius:12px;padding:12px 14px;font-size:14px;border:1px solid #f1d98a}
table{width:100%;border-collapse:collapse;margin:16px auto;background:var(--card);border:1px solid var(--line);border-radius:14px;overflow:hidden}
td{padding:12px 14px;border-bottom:1px solid var(--line)}tr:last-child td{border-bottom:0}td.r{text-align:right;font-variant-numeric:tabular-nums}
form{display:grid;gap:8px;margin-top:16px}button{min-height:48px;padding:12px 16px;border-radius:10px;border:1px solid var(--line);font:inherit;font-weight:600;cursor:pointer}
.pay{background:var(--accent);color:var(--on);border-color:var(--accent)}.alt{background:var(--card);color:var(--fg)}
:focus-visible{outline:3px solid var(--accent);outline-offset:2px}</style></head><body>
<p class="note">Preview: this is a simulated subscription checkout. No money moves. On the live app, customers subscribe on Stripe's checkout page.</p>
<h1>Subscribe: {{.total}} every {{.every}}</h1><p>{{.sub.Reference}} · {{.sub.Status}}{{if .sub.TrialDays}} · {{.sub.TrialDays}}-day free trial{{end}}{{if .sub.EndAfter}} · ends after {{.sub.EndAfter}} payments{{end}}</p>
{{if .sub.CurrentPeriodEnd}}<p>Next payment: {{.sub.CurrentPeriodEnd}} UTC{{if .sub.CancelAtPeriodEnd}} (ends then instead){{end}} · {{.sub.PaymentsMade}} paid so far</p>{{end}}
<table>{{range .items}}<tr><td>{{.Name}} × {{if .Quantity}}{{.Quantity}}{{else}}1{{end}}</td><td class="r">{{printf "%.2f" .Price}}</td></tr>{{end}}</table>
<form method="post"><input type="hidden" name="csrf" value="{{.csrf}}">
{{if eq .sub.Status "pending"}}<button class="pay" name="decision" value="pay">Subscribe (simulated)</button><button class="alt" name="decision" value="cancel">Cancel</button>
{{else if eq .sub.Status "canceled"}}<p>This subscription has ended.</p>
{{else}}<button class="pay" name="decision" value="renew">Simulate the next payment</button><button class="alt" name="decision" value="renewal_failed">Simulate a failed renewal</button>{{end}}
</form></body></html>`))

// AdminCancelSubscription cancels a live subscription from the admin
// panel, now or at the end of the paid period.
func (p *Platform) AdminCancelSubscription(ctx context.Context, app, id string, atPeriodEnd bool, by string) (Subscription, error) {
	st, err := p.liveStageForMoney(app)
	if err != nil {
		return Subscription{}, err
	}
	if _, err := subscriptionByID(ctx, st, id); err != nil {
		return Subscription{}, err
	}
	if err := p.allowMoney(st); err != nil {
		return Subscription{}, err
	}
	sub, err := p.cancelSubscription(ctx, st, id, atPeriodEnd, by)
	if err == nil {
		when := "now"
		if atPeriodEnd {
			when = "at the end of the paid period"
		}
		p.event(st, "admin", "warn", "", fmt.Sprintf("%s cancelled the subscription for %s in the admin panel (%s)", by, sub.Reference, when))
	}
	return sub, err
}

// RunningSubscriptions counts the live app's subscriptions that will still
// charge customers: not ended and not set to end at the period's end.
func (p *Platform) RunningSubscriptions(ctx context.Context, app string) (int, error) {
	st, err := p.loadStage(app, Live)
	if err != nil || st == nil {
		return 0, err
	}
	var n int
	err = st.admin.QueryRowContext(ctx, `SELECT count(*) FROM rx_subscriptions
		WHERE status IN ('active', 'trialing', 'past_due') AND NOT cancel_at_period_end`).Scan(&n)
	if err != nil && strings.Contains(err.Error(), "no such table") {
		return 0, nil
	}
	return n, err
}
