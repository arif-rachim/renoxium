package platform

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/arif-rachim/renoxium/internal/stripe"
)

// Money actions: refunds, captures and releases of card holds. Scripts
// call them through payments.refund, payments.capture and
// payments.release; owners through the admin panel's Payments page.

// MoneyActionsPerHour caps refunds, captures, releases and subscription
// cancellations per app stage, from scripts and the admin panel together,
// so a script in a loop cannot refund every payment at once.
const MoneyActionsPerHour = 60

// hookQueue holds on_payment runs started inside a script's own platform
// call; they run once that script has finished.
type hookQueue struct {
	mu  sync.Mutex
	fns []func(context.Context)
}

type hookQueueKey struct{}

func (q *hookQueue) add(fn func(context.Context)) {
	q.mu.Lock()
	q.fns = append(q.fns, fn)
	q.mu.Unlock()
}

// runDeferred runs the on_payment hooks queued while the script ran.
func (h *scriptHost) runDeferred(ctx context.Context) {
	ctx = context.WithoutCancel(ctx)
	for {
		h.hooks.mu.Lock()
		fns := h.hooks.fns
		h.hooks.fns = nil
		h.hooks.mu.Unlock()
		if len(fns) == 0 {
			return
		}
		for _, fn := range fns {
			fn(ctx)
		}
	}
}

// moneyLocks serialise money actions on one payment or subscription, so
// two clicks cannot refund the same amount twice.
var moneyLocks sync.Map

func lockMoney(st *stage, id string) func() {
	m, _ := moneyLocks.LoadOrStore(st.app+"/"+st.name+"/"+id, &sync.Mutex{})
	mu := m.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}

func (p *Platform) allowMoney(st *stage) error {
	if !p.allow("money:"+st.app+":"+st.name, MoneyActionsPerHour, time.Hour) {
		return fmt.Errorf("more than %d refunds, captures, releases or cancellations in an hour for this app; try again later", MoneyActionsPerHour)
	}
	return nil
}

// toMinor turns an amount in major units into the smallest unit, checking
// it is positive with at most 2 decimals. nil is 0 (= everything).
func toMinor(v *float64, currency, field string) (int64, error) {
	if v == nil {
		return 0, nil
	}
	minor := math.Round(*v * 100)
	if *v <= 0 || minor > 99999999 || math.Abs(*v*100-minor) > 1e-6 {
		return 0, fmt.Errorf("%s must be a positive amount in %s with at most 2 decimals, e.g. 250 or 99.5", field, strings.ToUpper(currency))
	}
	return int64(minor), nil
}

// parseAmountField reads an amount typed into an admin form ("" = all).
func parseAmountField(s string) (*float64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
		return nil, errors.New("enter the amount as a number, e.g. 100 or 99.50, or leave it empty for the whole amount")
	}
	return &v, nil
}

func (p *Platform) paymentByID(ctx context.Context, st *stage, id string) (Payment, error) {
	list, err := queryPayments(ctx, st, `WHERE id = ?`, id)
	if err != nil {
		return Payment{}, err
	}
	if len(list) == 0 {
		return Payment{}, fmt.Errorf("payment %q: %w", id, ErrNotFound)
	}
	return list[0], nil
}

// newestPayment is the newest payment for a reference in one of statuses.
func newestPayment(ctx context.Context, st *stage, ref string, statuses ...string) (Payment, bool, error) {
	args := []any{ref}
	marks := make([]string, len(statuses))
	for i, s := range statuses {
		marks[i] = "?"
		args = append(args, s)
	}
	list, err := queryPayments(ctx, st, `WHERE reference = ? AND status IN (`+strings.Join(marks, ", ")+`) ORDER BY created_at DESC, rowid DESC LIMIT 1`, args...)
	if err != nil || len(list) == 0 {
		return Payment{}, false, err
	}
	return list[0], true, nil
}

// connected returns the app's Stripe account for live money actions.
func (p *Platform) connected(st *stage) (string, error) {
	if p.cfg.Stripe == nil {
		return "", errors.New("payments are not set up on this Renoxium server")
	}
	pa, err := p.PaymentAccount(st.app)
	if err != nil {
		return "", err
	}
	if pa.StripeAccount == "" {
		return "", errors.New("the owner has not connected Stripe (Renoxium admin panel, Payments)")
	}
	return pa.StripeAccount, nil
}

type moneyInput struct {
	Reference string   `json:"reference"`
	Amount    *float64 `json:"amount"`
	Reason    string   `json:"reason"`
}

func decodeMoney(arg any, fn string) (moneyInput, error) {
	var in moneyInput
	if s, ok := arg.(string); ok {
		in.Reference = s
	} else if err := decodeArg(arg, &in); err != nil {
		return in, fmt.Errorf("payments.%s expects {reference, amount}", fn)
	}
	if strings.TrimSpace(in.Reference) == "" {
		return in, fmt.Errorf("payments.%s needs the reference used in payments.checkout", fn)
	}
	return in, nil
}

func moneyResult(pm Payment) map[string]any {
	out := map[string]any{"id": pm.ID, "reference": pm.Reference, "status": pm.Status, "amount": pm.Amount,
		"refunded_amount": pm.Refunded, "currency": pm.Currency}
	if pm.Hold {
		out["authorized_amount"] = pm.Authorized
	}
	return out
}

// scriptRefund answers payments.refund({reference, amount?, reason?}).
func (p *Platform) scriptRefund(ctx context.Context, st *stage, arg any) (any, error) {
	in, err := decodeMoney(arg, "refund")
	if err != nil {
		return nil, err
	}
	pm, ok, err := newestPayment(ctx, st, in.Reference, "paid", "partially_refunded")
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("no paid payment for reference %q to refund", in.Reference)
	}
	minor, err := toMinor(in.Amount, pm.Currency, "amount")
	if err != nil {
		return nil, err
	}
	if err := p.allowMoney(st); err != nil {
		return nil, err
	}
	pm, err = p.refundPayment(ctx, st, pm.ID, minor, in.Reason, "a script")
	if err != nil {
		return nil, err
	}
	return moneyResult(pm), nil
}

// refundPayment refunds minor (0 = what is left) of a paid payment. On live
// it is a refund on the merchant's account (refund_application_fee, so
// Renoxium's Free-plan fee is returned in proportion); on preview it is
// simulated. The refund is recorded at once and on_payment runs with
// partially_refunded or refunded.
func (p *Platform) refundPayment(ctx context.Context, st *stage, id string, minor int64, reason, by string) (Payment, error) {
	defer lockMoney(st, id)()
	pm, err := p.paymentByID(ctx, st, id)
	if err != nil {
		return pm, err
	}
	if pm.Status != "paid" && pm.Status != "partially_refunded" {
		return pm, fmt.Errorf("payment for %s is %s; only paid payments can be refunded", pm.Reference, pm.Status)
	}
	left := pm.minor - pm.refundedMinor
	if left <= 0 {
		return pm, fmt.Errorf("payment for %s is already refunded in full", pm.Reference)
	}
	if minor == 0 {
		minor = left
	}
	if minor > left {
		return pm, fmt.Errorf("at most %.2f %s of the payment for %s is left to refund", float64(left)/100, strings.ToUpper(pm.Currency), pm.Reference)
	}
	reason = strings.TrimSpace(reason)
	if utf8.RuneCountInString(reason) > 200 {
		return pm, errors.New("reason must be at most 200 characters")
	}
	if st.name == Live {
		account, err := p.connected(st)
		if err != nil {
			return pm, err
		}
		pi := pm.paymentIntent
		if pi == "" && pm.Subscription != "" {
			if pi, err = p.cfg.Stripe.InvoicePaymentIntent(ctx, account, pm.ID); err == nil && pi != "" {
				st.admin.ExecContext(ctx, `UPDATE rx_payments SET payment_intent = ? WHERE id = ?`, pi, pm.ID)
			}
		}
		if pi == "" {
			return pm, fmt.Errorf("the payment for %s cannot be refunded from Renoxium; refund it in the Stripe Dashboard", pm.Reference)
		}
		meta := map[string]string{"rx_app": st.app, "rx_reference": pm.Reference, "rx_by": by}
		if reason != "" && !stripe.RefundReasons[reason] {
			meta["rx_reason"] = reason
		}
		if _, err := p.cfg.Stripe.CreateRefund(ctx, account, pi, minor, reason, pm.fee > 0, meta); err != nil {
			p.event(st, "payment", "error", "", fmt.Sprintf("refund of %s not made: %v", pm.Reference, err))
			return pm, err
		}
	}
	total := pm.refundedMinor + minor
	p.event(st, "payment", "warn", "", fmt.Sprintf("%s refunded %s %.2f of the payment for %s%s", by, strings.ToUpper(pm.Currency),
		float64(minor)/100, pm.Reference, reasonSuffix(reason)))
	if err := p.applyRefund(ctx, st, pm.ID, total, total >= pm.minor); err != nil {
		return pm, err
	}
	return p.paymentByID(ctx, st, pm.ID)
}

func reasonSuffix(reason string) string {
	if reason == "" {
		return ""
	}
	return " (" + reason + ")"
}

// scriptCapture answers payments.capture({reference, amount?}).
func (p *Platform) scriptCapture(ctx context.Context, st *stage, arg any) (any, error) {
	in, err := decodeMoney(arg, "capture")
	if err != nil {
		return nil, err
	}
	pm, ok, err := newestPayment(ctx, st, in.Reference, "authorized")
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("no card hold in place for reference %q", in.Reference)
	}
	minor, err := toMinor(in.Amount, pm.Currency, "amount")
	if err != nil {
		return nil, err
	}
	if err := p.allowMoney(st); err != nil {
		return nil, err
	}
	pm, err = p.capturePayment(ctx, st, pm.ID, minor, "a script")
	if err != nil {
		return nil, err
	}
	return moneyResult(pm), nil
}

// capturePayment takes minor (0 = all) of a card hold; the rest of the
// hold is released. On the Free plan the fee is 2% of what is taken.
func (p *Platform) capturePayment(ctx context.Context, st *stage, id string, minor int64, by string) (Payment, error) {
	defer lockMoney(st, id)()
	pm, err := p.paymentByID(ctx, st, id)
	if err != nil {
		return pm, err
	}
	if pm.Status != "authorized" {
		return pm, fmt.Errorf("payment for %s is %s; only a card hold in place can be captured", pm.Reference, pm.Status)
	}
	if minor == 0 {
		minor = pm.authorizedMinor
	}
	if minor > pm.authorizedMinor {
		return pm, fmt.Errorf("at most the %.2f %s held can be captured", float64(pm.authorizedMinor)/100, strings.ToUpper(pm.Currency))
	}
	fee := int64(0)
	if pm.fee > 0 {
		fee = int64(math.Round(float64(minor) * FreePlanFeePercent / 100))
	}
	if st.name == Live {
		account, err := p.connected(st)
		if err != nil {
			return pm, err
		}
		if pm.paymentIntent == "" {
			return pm, fmt.Errorf("the hold for %s is not confirmed by Stripe yet", pm.Reference)
		}
		if _, err := p.cfg.Stripe.CapturePaymentIntent(ctx, account, pm.paymentIntent, minor, fee); err != nil {
			p.event(st, "payment", "error", "", fmt.Sprintf("capture of %s failed: %v", pm.Reference, err))
			return pm, err
		}
	}
	if err := p.recordCapture(ctx, st, pm.ID, minor, fee, by); err != nil {
		return pm, err
	}
	return p.paymentByID(ctx, st, pm.ID)
}

// recordCapture marks a hold paid with the amount taken, once.
func (p *Platform) recordCapture(ctx context.Context, st *stage, id string, minor, fee int64, by string) error {
	res, err := st.admin.ExecContext(ctx, `UPDATE rx_payments SET status = 'paid', amount = ?, application_fee = ?, updated_at = datetime('now')
		WHERE id = ? AND status = 'authorized'`, minor, fee, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil
	}
	p.event(st, "payment", "info", "", fmt.Sprintf("%s captured %.2f of card hold %s", by, float64(minor)/100, id))
	return p.afterStatusChange(ctx, st, id, "paid")
}

// scriptRelease answers payments.release({reference}).
func (p *Platform) scriptRelease(ctx context.Context, st *stage, arg any) (any, error) {
	in, err := decodeMoney(arg, "release")
	if err != nil {
		return nil, err
	}
	pm, ok, err := newestPayment(ctx, st, in.Reference, "authorized")
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("no card hold in place for reference %q", in.Reference)
	}
	if err := p.allowMoney(st); err != nil {
		return nil, err
	}
	pm, err = p.releasePayment(ctx, st, pm.ID, "a script")
	if err != nil {
		return nil, err
	}
	return moneyResult(pm), nil
}

// releasePayment cancels a card hold; nothing is taken.
func (p *Platform) releasePayment(ctx context.Context, st *stage, id, by string) (Payment, error) {
	defer lockMoney(st, id)()
	pm, err := p.paymentByID(ctx, st, id)
	if err != nil {
		return pm, err
	}
	if pm.Status != "authorized" {
		return pm, fmt.Errorf("payment for %s is %s; only a card hold in place can be released", pm.Reference, pm.Status)
	}
	if st.name == Live {
		account, err := p.connected(st)
		if err != nil {
			return pm, err
		}
		if pm.paymentIntent == "" {
			return pm, fmt.Errorf("the hold for %s is not confirmed by Stripe yet", pm.Reference)
		}
		if _, err := p.cfg.Stripe.CancelPaymentIntent(ctx, account, pm.paymentIntent); err != nil {
			p.event(st, "payment", "error", "", fmt.Sprintf("release of %s failed: %v", pm.Reference, err))
			return pm, err
		}
	}
	if err := p.recordRelease(ctx, st, pm.ID, by); err != nil {
		return pm, err
	}
	return p.paymentByID(ctx, st, pm.ID)
}

func (p *Platform) recordRelease(ctx context.Context, st *stage, id, by string) error {
	res, err := st.admin.ExecContext(ctx, `UPDATE rx_payments SET status = 'released', updated_at = datetime('now') WHERE id = ? AND status = 'authorized'`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil
	}
	p.event(st, "payment", "info", "", fmt.Sprintf("%s released card hold %s", by, id))
	return p.afterStatusChange(ctx, st, id, "released")
}

// holdAuthorized applies checkout.session.completed for a card hold: it
// reads the PaymentIntent to confirm the hold and its expiry. handled is
// false when the session is not a hold.
func (p *Platform) holdAuthorized(ctx context.Context, app, account string, s stripe.Session) (handled bool, err error) {
	st, err := p.loadStage(app, Live)
	if err != nil || st == nil {
		return false, err
	}
	var hold bool
	if err := st.admin.QueryRowContext(ctx, `SELECT hold FROM rx_payments WHERE id = ?`, s.ID).Scan(&hold); err != nil || !hold {
		return false, nil
	}
	if s.PaymentIntent == "" {
		return true, nil
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
	defer cancel()
	pi, err := p.cfg.Stripe.GetPaymentIntent(ctx, account, s.PaymentIntent)
	if err != nil {
		return true, err // Stripe retries the webhook
	}
	switch pi.Status {
	case "requires_capture":
		expires := ""
		if t := pi.CaptureBefore(); !t.IsZero() {
			expires = t.UTC().Format("2006-01-02 15:04:05")
		}
		st.admin.ExecContext(ctx, `UPDATE rx_payments SET authorized = ?, hold_expires_at = ? WHERE id = ? AND status = 'pending'`,
			pi.AmountCapturable, expires, s.ID)
		return true, p.settle(ctx, st, s.ID, "authorized", s.Customer.Email, s.Customer.Name, s.PaymentIntent)
	case "succeeded":
		return true, p.settle(ctx, st, s.ID, "paid", s.Customer.Email, s.Customer.Name, s.PaymentIntent)
	}
	return true, nil
}

// dedupeEvent reports whether an event type is applied once per event id.
func dedupeEvent(typ string) bool {
	return strings.HasPrefix(typ, "invoice.") || strings.HasPrefix(typ, "customer.subscription.") || strings.HasPrefix(typ, "payment_intent.")
}

func (p *Platform) seenStripeEvent(ctx context.Context, id string) bool {
	if id == "" {
		return false
	}
	var n int
	p.db.QueryRowContext(ctx, `SELECT count(*) FROM stripe_events WHERE id = ?`, id).Scan(&n)
	return n > 0
}

func (p *Platform) markStripeEvent(ctx context.Context, id string) {
	if id == "" {
		return
	}
	p.db.ExecContext(ctx, `INSERT OR IGNORE INTO stripe_events (id, at) VALUES (?, ?)`, id, now())
	// Stripe retries for up to 3 days; keep a month.
	p.db.ExecContext(ctx, `DELETE FROM stripe_events WHERE at < ?`, time.Now().UTC().AddDate(0, -1, 0).Format(time.RFC3339))
}

// appOfAccount finds the live stage of the app connected to a Stripe
// account, or nil.
func (p *Platform) appOfAccount(ctx context.Context, account string) (*stage, error) {
	if account == "" {
		return nil, nil
	}
	var app string
	if err := p.db.QueryRowContext(ctx, `SELECT app FROM payment_accounts WHERE stripe_account = ?`, account).Scan(&app); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return p.loadStage(app, Live)
}

// handleMoneyEvent applies PaymentIntent, invoice and subscription events
// from connected accounts.
func (p *Platform) handleMoneyEvent(ctx context.Context, e stripe.Event) error {
	st, err := p.appOfAccount(ctx, e.Account)
	if err != nil || st == nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
	defer cancel()
	switch e.Type {
	case "payment_intent.succeeded", "payment_intent.canceled":
		// A hold captured or cancelled in the Stripe Dashboard, or expired.
		var pi stripe.PaymentIntent
		if json.Unmarshal(e.Data.Object, &pi) != nil || pi.ID == "" {
			return nil
		}
		var id string
		var fee int64
		err := st.admin.QueryRowContext(ctx, `SELECT id, application_fee FROM rx_payments WHERE payment_intent = ? AND hold = 1 AND status = 'authorized'`, pi.ID).Scan(&id, &fee)
		if err != nil {
			return nil
		}
		if e.Type == "payment_intent.canceled" {
			return p.recordRelease(ctx, st, id, "Stripe")
		}
		if fee > 0 {
			fee = int64(math.Round(float64(pi.AmountReceived) * FreePlanFeePercent / 100))
		}
		return p.recordCapture(ctx, st, id, pi.AmountReceived, fee, "Stripe")
	case "invoice.paid", "invoice.payment_failed":
		var in stripe.Invoice
		if json.Unmarshal(e.Data.Object, &in) != nil || in.ID == "" {
			return nil
		}
		return p.invoiceEvent(ctx, st, e.Account, e.Type == "invoice.paid", in)
	case "customer.subscription.created", "customer.subscription.updated", "customer.subscription.deleted":
		var s stripe.Subscription
		if json.Unmarshal(e.Data.Object, &s) != nil || s.ID == "" {
			return nil
		}
		return p.subscriptionEvent(ctx, st, s, e.Type == "customer.subscription.deleted")
	}
	return nil
}

// --- Admin panel ---

// AdminRefund refunds a live payment from the admin panel; amount "" is
// what is left. by is the owner's email, for the log.
func (p *Platform) AdminRefund(ctx context.Context, app, id, amount, by string) (Payment, error) {
	st, err := p.liveStageForMoney(app)
	if err != nil {
		return Payment{}, err
	}
	pm, err := p.paymentByID(ctx, st, id)
	if err != nil {
		return pm, err
	}
	v, err := parseAmountField(amount)
	if err != nil {
		return pm, err
	}
	minor, err := toMinor(v, pm.Currency, "the amount")
	if err != nil {
		return pm, err
	}
	if err := p.allowMoney(st); err != nil {
		return pm, err
	}
	pm, err = p.refundPayment(ctx, st, id, minor, "requested_by_customer", by)
	if err == nil {
		p.event(st, "admin", "warn", "", fmt.Sprintf("%s refunded the payment for %s in the admin panel (%.2f %s refunded in all)", by, pm.Reference,
			pm.Refunded, strings.ToUpper(pm.Currency)))
	}
	return pm, err
}

// AdminCapture takes all or part of a live card hold.
func (p *Platform) AdminCapture(ctx context.Context, app, id, amount, by string) (Payment, error) {
	st, err := p.liveStageForMoney(app)
	if err != nil {
		return Payment{}, err
	}
	pm, err := p.paymentByID(ctx, st, id)
	if err != nil {
		return pm, err
	}
	v, err := parseAmountField(amount)
	if err != nil {
		return pm, err
	}
	minor, err := toMinor(v, pm.Currency, "the amount")
	if err != nil {
		return pm, err
	}
	if err := p.allowMoney(st); err != nil {
		return pm, err
	}
	pm, err = p.capturePayment(ctx, st, id, minor, by)
	if err == nil {
		p.event(st, "admin", "info", "", fmt.Sprintf("%s captured %.2f %s of the card hold for %s in the admin panel", by, pm.Amount,
			strings.ToUpper(pm.Currency), pm.Reference))
	}
	return pm, err
}

// AdminRelease releases a live card hold.
func (p *Platform) AdminRelease(ctx context.Context, app, id, by string) (Payment, error) {
	st, err := p.liveStageForMoney(app)
	if err != nil {
		return Payment{}, err
	}
	if err := p.allowMoney(st); err != nil {
		return Payment{}, err
	}
	pm, err := p.releasePayment(ctx, st, id, by)
	if err == nil {
		p.event(st, "admin", "info", "", fmt.Sprintf("%s released the card hold for %s in the admin panel", by, pm.Reference))
	}
	return pm, err
}

func (p *Platform) liveStageForMoney(app string) (*stage, error) {
	st, err := p.loadStage(app, Live)
	if err != nil {
		return nil, err
	}
	if st == nil {
		return nil, fmt.Errorf("app %q is not published yet", app)
	}
	return st, nil
}
