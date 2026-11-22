package platform

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"math"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/arif-rachim/renoxium/internal/sandbox"
	"github.com/arif-rachim/renoxium/internal/stripe"
)

// PayPrefix serves the simulated checkout on previews.
const PayPrefix = "/_rx/pay/"

// Two-decimal currencies scripts may charge in.
var currencies = map[string]bool{"aed": true, "usd": true, "eur": true, "gbp": true, "sar": true}

// PaymentAccount is an app's link to its owner's Stripe account.
type PaymentAccount struct {
	StripeConfigured bool   // the server has a Stripe key
	StripeAccount    string // acct_..., empty before the owner connects
	ChargesEnabled   bool
	DetailsSubmitted bool
}

// Payment is one checkout as the owner and scripts see it.
type Payment struct {
	ID            string  `json:"id"`
	Reference     string  `json:"reference"`
	Status        string  `json:"status"` // pending, paid, failed, expired
	Amount        float64 `json:"amount"` // in major units, e.g. 250.5 AED
	Currency      string  `json:"currency"`
	CustomerEmail string  `json:"customer_email"`
	CustomerName  string  `json:"customer_name"`
	Refunded      float64 `json:"refunded_amount"` // refunded so far, in major units
	CreatedAt     string  `json:"created_at"`
	UpdatedAt     string  `json:"updated_at,omitempty"`
	// Card holds (payments.checkout with hold: true).
	Hold          bool    `json:"hold,omitempty"`
	Authorized    float64 `json:"authorized_amount,omitempty"` // amount held on the card
	HoldExpiresAt string  `json:"hold_expires_at,omitempty"`   // UTC "YYYY-MM-DD HH:MM:SS", when known
	// Subscription invoices (payments.subscribe).
	Subscription string `json:"subscription,omitempty"`
	PeriodStart  string `json:"period_start,omitempty"`
	PeriodEnd    string `json:"period_end,omitempty"`

	minor, refundedMinor, authorizedMinor, fee int64
	paymentIntent                              string
}

// Refundable is what is left to refund, in major units.
func (pm Payment) Refundable() float64 {
	if pm.Status != "paid" && pm.Status != "partially_refunded" {
		return 0
	}
	return float64(pm.minor-pm.refundedMinor) / 100
}

func (p *Platform) ensurePaymentTables() error {
	_, err := p.db.Exec(`CREATE TABLE IF NOT EXISTS payment_accounts (app TEXT PRIMARY KEY, stripe_account TEXT NOT NULL UNIQUE,
		charges_enabled INTEGER NOT NULL DEFAULT 0, details_submitted INTEGER NOT NULL DEFAULT 0, updated_at TEXT NOT NULL)`)
	if err == nil {
		// Connect webhook events already applied, by event id.
		_, err = p.db.Exec(`CREATE TABLE IF NOT EXISTS stripe_events (id TEXT PRIMARY KEY, at TEXT NOT NULL)`)
	}
	return err
}

// PaymentAccount returns the app's Stripe link.
func (p *Platform) PaymentAccount(app string) (PaymentAccount, error) {
	pa := PaymentAccount{StripeConfigured: p.cfg.Stripe != nil}
	err := p.db.QueryRow(`SELECT stripe_account, charges_enabled, details_submitted FROM payment_accounts WHERE app = ?`, app).
		Scan(&pa.StripeAccount, &pa.ChargesEnabled, &pa.DetailsSubmitted)
	if errors.Is(err, sql.ErrNoRows) {
		err = nil
	}
	return pa, err
}

func (p *Platform) savePaymentAccount(app string, a stripe.Account) error {
	_, err := p.db.Exec(`INSERT INTO payment_accounts (app, stripe_account, charges_enabled, details_submitted, updated_at) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (app) DO UPDATE SET charges_enabled = excluded.charges_enabled, details_submitted = excluded.details_submitted,
		updated_at = excluded.updated_at`, app, a.ID, a.ChargesEnabled, a.DetailsSubmitted, now())
	return err
}

// StripeOnboardingURL returns where the owner sets up or finishes their
// Stripe account. The first call creates the account: the merchant gets the
// full Stripe Dashboard, pays Stripe's fees, and Stripe covers losses.
func (p *Platform) StripeOnboardingURL(ctx context.Context, app, ownerEmail, returnURL, refreshURL string) (string, error) {
	if p.cfg.Stripe == nil {
		return "", errors.New("payments are not set up on this Renoxium server (STRIPE_SECRET_KEY)")
	}
	pa, err := p.PaymentAccount(app)
	if err != nil {
		return "", err
	}
	if pa.StripeAccount == "" {
		a, err := p.cfg.Stripe.CreateAccount(ctx, ownerEmail, "AE")
		if err != nil {
			return "", err
		}
		if err := p.savePaymentAccount(app, a); err != nil {
			return "", err
		}
		pa.StripeAccount = a.ID
	}
	return p.cfg.Stripe.OnboardingLink(ctx, pa.StripeAccount, refreshURL, returnURL)
}

// RefreshPaymentAccount reads the account's state from Stripe.
func (p *Platform) RefreshPaymentAccount(ctx context.Context, app string) (PaymentAccount, error) {
	pa, err := p.PaymentAccount(app)
	if err != nil || pa.StripeAccount == "" || p.cfg.Stripe == nil {
		return pa, err
	}
	a, err := p.cfg.Stripe.GetAccount(ctx, pa.StripeAccount)
	if err != nil {
		return pa, err
	}
	if err := p.savePaymentAccount(app, a); err != nil {
		return pa, err
	}
	return p.PaymentAccount(app)
}

// Payments lists an app's newest live payments.
func (p *Platform) Payments(ctx context.Context, app string, limit int) ([]Payment, error) {
	st, err := p.loadStage(app, Live)
	if err != nil || st == nil {
		return nil, err
	}
	return queryPayments(ctx, st, `ORDER BY created_at DESC, rowid DESC LIMIT ?`, limit)
}

func queryPayments(ctx context.Context, st *stage, tail string, args ...any) ([]Payment, error) {
	rows, err := st.admin.QueryContext(ctx, `SELECT id, reference, status, amount, currency, customer_email, customer_name, created_at,
		coalesce(updated_at, ''), refunded, hold, authorized, hold_expires_at, application_fee, subscription, period_start, period_end,
		payment_intent FROM rx_payments `+tail, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Payment{}
	for rows.Next() {
		var pm Payment
		if err := rows.Scan(&pm.ID, &pm.Reference, &pm.Status, &pm.minor, &pm.Currency, &pm.CustomerEmail, &pm.CustomerName, &pm.CreatedAt, &pm.UpdatedAt,
			&pm.refundedMinor, &pm.Hold, &pm.authorizedMinor, &pm.HoldExpiresAt, &pm.fee, &pm.Subscription, &pm.PeriodStart, &pm.PeriodEnd,
			&pm.paymentIntent); err != nil {
			return nil, err
		}
		pm.Amount = float64(pm.minor) / 100
		pm.Refunded = float64(pm.refundedMinor) / 100
		if pm.Hold {
			pm.Authorized = float64(pm.authorizedMinor) / 100
		}
		out = append(out, pm)
	}
	return out, rows.Err()
}

type checkoutItem struct {
	Name     string  `json:"name"`
	Price    float64 `json:"price"`
	Quantity int64   `json:"quantity"`
}

type checkoutInput struct {
	Items         []checkoutItem `json:"items"`
	Reference     string         `json:"reference"`
	Currency      string         `json:"currency"`
	SuccessPath   string         `json:"success_path"`
	CancelPath    string         `json:"cancel_path"`
	CustomerEmail string         `json:"customer_email"`
	// Hold only authorises the card; capture or release it later.
	Hold bool `json:"hold"`
}

// decodeArg turns a script argument into a typed value through JSON.
func decodeArg(arg any, out any) error {
	raw, err := json.Marshal(arg)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, out)
}

func (in *checkoutInput) validate() ([]stripe.LineItem, int64, error) {
	in.Currency = strings.ToLower(strings.TrimSpace(in.Currency))
	if in.Currency == "" {
		in.Currency = "aed"
	}
	switch {
	case !currencies[in.Currency]:
		return nil, 0, fmt.Errorf("currency %q is not supported; use aed, usd, eur, gbp or sar", in.Currency)
	case in.Reference == "" || utf8.RuneCountInString(in.Reference) > 100:
		return nil, 0, errors.New("reference is required (your order number, at most 100 characters)")
	case len(in.Items) == 0 || len(in.Items) > 50:
		return nil, 0, errors.New("items must hold between 1 and 50 lines")
	}
	in.SuccessPath, in.CancelPath = localPath(orDefault(in.SuccessPath, "/")), localPath(orDefault(in.CancelPath, "/"))
	var items []stripe.LineItem
	var total int64
	for i, it := range in.Items {
		minor := math.Round(it.Price * 100)
		switch {
		case strings.TrimSpace(it.Name) == "" || utf8.RuneCountInString(it.Name) > 120:
			return nil, 0, fmt.Errorf("items[%d].name is required (at most 120 characters)", i)
		case it.Price <= 0 || minor > 99999999 || math.Abs(it.Price*100-minor) > 1e-6:
			return nil, 0, fmt.Errorf("items[%d].price must be a positive amount in %s with at most 2 decimals, e.g. 250 or 99.5", i, strings.ToUpper(in.Currency))
		case it.Quantity == 0:
			it.Quantity = 1
		}
		if it.Quantity < 1 || it.Quantity > 999 {
			return nil, 0, fmt.Errorf("items[%d].quantity must be between 1 and 999", i)
		}
		items = append(items, stripe.LineItem{Name: it.Name, Amount: int64(minor), Quantity: it.Quantity})
		total += int64(minor) * it.Quantity
	}
	return items, total, nil
}

func orDefault(s, d string) string {
	if strings.TrimSpace(s) == "" {
		return d
	}
	return s
}

// checkout starts a payment. Live creates a Stripe Checkout Session on the
// owner's account (a direct charge); preview creates a simulated checkout so
// nothing is charged.
func (p *Platform) checkout(ctx context.Context, st *stage, arg any) (any, error) {
	var in checkoutInput
	if err := decodeArg(arg, &in); err != nil {
		return nil, errors.New("payments.checkout expects {items: [{name, price, quantity}], reference, success_path, cancel_path}")
	}
	items, total, err := in.validate()
	if err != nil {
		return nil, err
	}
	itemsJSON, _ := json.Marshal(in.Items)
	var id, url string
	fee := int64(0)
	if st.name == Preview {
		id = "sim_" + secretToken()[:24]
		url = PayPrefix + id
	} else {
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
		base := p.URL(st.app, Live)
		fee = p.applicationFee(st.app, total)
		s, err := p.cfg.Stripe.CreateCheckout(ctx, pa.StripeAccount, stripe.CheckoutParams{
			Currency: in.Currency, Items: items, SuccessURL: base + in.SuccessPath, CancelURL: base + in.CancelPath,
			CustomerEmail: in.CustomerEmail, Reference: in.Reference,
			Metadata:       map[string]string{"rx_app": st.app, "rx_reference": in.Reference},
			ApplicationFee: fee, Hold: in.Hold,
		})
		if err != nil {
			p.event(st, "payment", "error", "", "checkout not created: "+err.Error())
			return nil, err
		}
		id, url = s.ID, s.URL
	}
	_, err = st.admin.ExecContext(ctx, `INSERT INTO rx_payments (id, reference, status, amount, currency, items, success_path, cancel_path, customer_email,
		hold, authorized, application_fee) VALUES (?, ?, 'pending', ?, ?, ?, ?, ?, ?, ?, ?, ?)`, id, in.Reference, total, in.Currency, string(itemsJSON),
		in.SuccessPath, in.CancelPath, in.CustomerEmail, in.Hold, total, fee)
	if err != nil {
		return nil, err
	}
	kind := "checkout"
	if in.Hold {
		kind = "card hold"
	}
	p.event(st, "payment", "info", "", fmt.Sprintf("%s %s started for %s: %.2f %s", kind, id, in.Reference, float64(total)/100, strings.ToUpper(in.Currency)))
	out := map[string]any{"url": url, "id": id, "reference": in.Reference, "amount": float64(total) / 100, "currency": in.Currency}
	if in.Hold {
		out["hold"] = true
	}
	return out, nil
}

// paymentStatus answers payments.get(reference): the newest payment for it.
func (p *Platform) paymentStatus(ctx context.Context, st *stage, arg any) (any, error) {
	ref, ok := arg.(string)
	if !ok || ref == "" {
		return nil, errors.New("payments.get expects the reference used in payments.checkout")
	}
	list, err := queryPayments(ctx, st, `WHERE reference = ? ORDER BY CASE WHEN status IN ('paid', 'partially_refunded', 'refunded', 'authorized') THEN 0 ELSE 1 END,
		created_at DESC, rowid DESC LIMIT 1`, ref)
	if err != nil || len(list) == 0 {
		return nil, err
	}
	return list[0], nil
}

// settle records a payment's new status. On a change it runs the app's
// on_payment script once with the payment in req.body.
func (p *Platform) settle(ctx context.Context, st *stage, id, status, email, name, paymentIntent string) error {
	res, err := st.admin.ExecContext(ctx, `UPDATE rx_payments SET status = ?, customer_email = CASE WHEN ? != '' THEN ? ELSE customer_email END,
		customer_name = CASE WHEN ? != '' THEN ? ELSE customer_name END, payment_intent = CASE WHEN ? != '' THEN ? ELSE payment_intent END,
		updated_at = datetime('now')
		WHERE id = ? AND status != ? AND status NOT IN ('paid', 'refunded', 'partially_refunded', 'released')`,
		status, email, email, name, name, paymentIntent, paymentIntent, id, status)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil // already recorded, e.g. a repeated webhook
	}
	return p.afterStatusChange(ctx, st, id, status)
}

// afterStatusChange tells the owner about a payment's new status and runs
// on_payment with it as the event.
func (p *Platform) afterStatusChange(ctx context.Context, st *stage, id, event string) error {
	list, err := queryPayments(ctx, st, `WHERE id = ?`, id)
	if err != nil || len(list) == 0 {
		return err
	}
	pm := list[0]
	p.event(st, "payment", "info", "", fmt.Sprintf("payment %s for %s is %s", id, pm.Reference, event))
	if event == "paid" && pm.minor > 0 {
		if st.name == Live {
			p.funnel(p.ownerOf(st.app), FunnelActivity, st.app, "card")
			p.noteOwnerActive(p.ownerOf(st.app))
		}
		subject := fmt.Sprintf("Payment received: %s %.2f for %s", strings.ToUpper(pm.Currency), pm.Amount, pm.Reference)
		p.whatsappOwner(st, subject)
		p.mailOwner(ctx, st, subject,
			fmt.Sprintf("A card payment was completed.\n\nReference: %s\nAmount: %s %.2f\nCustomer: %s %s\n\nThe money is in your Stripe account.",
				pm.Reference, strings.ToUpper(pm.Currency), pm.Amount, pm.CustomerName, pm.CustomerEmail))
	}
	p.runPaymentHook(ctx, st, pm, event)
	return nil
}

// paymentHookBody is req.body of on_payment for a payment.
func paymentHookBody(pm Payment, event string) map[string]any {
	body := map[string]any{"event": event, "id": pm.ID, "reference": pm.Reference, "status": pm.Status, "amount": pm.Amount,
		"refunded_amount": pm.Refunded, "currency": pm.Currency, "customer_email": pm.CustomerEmail, "customer_name": pm.CustomerName}
	if pm.Hold {
		body["hold"] = true
		body["authorized_amount"] = pm.Authorized
		body["hold_expires_at"] = pm.HoldExpiresAt
	}
	if pm.Subscription != "" {
		body["subscription"] = pm.Subscription
		body["period"] = map[string]any{"start": pm.PeriodStart, "end": pm.PeriodEnd}
	}
	return body
}

// runPaymentHook runs the app's on_payment script with the payment.
func (p *Platform) runPaymentHook(ctx context.Context, st *stage, pm Payment, event string) {
	p.runHook(ctx, st, paymentHookBody(pm, event))
}

// runHook runs on_payment with body. Inside a script's own platform call
// (e.g. payments.refund) it waits until that script has finished, so the
// hook gets a script slot of its own.
func (p *Platform) runHook(ctx context.Context, st *stage, body map[string]any) {
	if st.def.OnPayment == "" {
		return
	}
	if q, ok := ctx.Value(hookQueueKey{}).(*hookQueue); ok && q != nil {
		q.add(func(ctx context.Context) { p.runHook(ctx, st, body) })
		return
	}
	release, err := p.acquireSlot(ctx, st)
	if err != nil {
		p.event(st, "payment", "error", "", "on_payment script not run: "+err.Error())
		return
	}
	rid := newRequestID()
	host := &scriptHost{st: st, p: p, rid: rid}
	_, err = p.runScript(ctx, st, "hook on_payment", st.def.Scripts[st.def.OnPayment], sandbox.Request{Method: http.MethodPost, Path: "/_rx/payment", Body: body, Origin: p.URL(st.app, st.name),
		Params: map[string]string{}, Query: map[string]string{}, Form: map[string]string{}}, host)
	release()
	host.runDeferred(ctx)
	if err != nil {
		p.event(st, "payment", "error", rid, "on_payment script "+st.def.OnPayment+": "+err.Error())
	}
}

// refund records a refund made in the merchant's Stripe Dashboard, found by
// its PaymentIntent, and runs on_payment when the refunded amount grows.
func (p *Platform) refund(ctx context.Context, st *stage, paymentIntent string, refundedMinor int64, full bool) error {
	var id string
	err := st.admin.QueryRowContext(ctx, `SELECT id FROM rx_payments WHERE payment_intent = ? AND payment_intent != ''`, paymentIntent).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	return p.applyRefund(ctx, st, id, refundedMinor, full)
}

// applyRefund records that refundedMinor of a payment is refunded in all
// and runs on_payment, once, when that amount grows. Refunds started by
// payments.refund or the admin panel are recorded at once, so the webhook
// that follows for the same amount changes nothing.
func (p *Platform) applyRefund(ctx context.Context, st *stage, id string, refundedMinor int64, full bool) error {
	status := "partially_refunded"
	if full {
		status = "refunded"
	}
	res, err := st.admin.ExecContext(ctx, `UPDATE rx_payments SET status = ?, refunded = ?, updated_at = datetime('now')
		WHERE id = ? AND status IN ('paid', 'partially_refunded') AND refunded < ?`,
		status, refundedMinor, id, refundedMinor)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil
	}
	list, err := queryPayments(ctx, st, `WHERE id = ?`, id)
	if err != nil || len(list) == 0 {
		return err
	}
	pm := list[0]
	p.event(st, "payment", "warn", "", fmt.Sprintf("payment for %s was %s: %s %.2f", pm.Reference, strings.ReplaceAll(status, "_", " "),
		strings.ToUpper(pm.Currency), pm.Refunded))
	p.runPaymentHook(ctx, st, pm, status)
	return nil
}

// servePay is the simulated checkout of previews. Live apps never serve it.
func (p *Platform) servePay(w http.ResponseWriter, r *http.Request, st *stage) {
	if st.name != Preview {
		http.NotFound(w, r)
		return
	}
	id := strings.TrimPrefix(r.URL.Path, PayPrefix)
	if strings.HasPrefix(id, subscriptionPrefix) {
		p.serveSubscriptionPay(w, r, st, id)
		return
	}
	var ref, status, itemsJSON, currency, success, cancel string
	var minor int64
	var hold bool
	err := st.admin.QueryRowContext(r.Context(), `SELECT reference, status, items, amount, currency, success_path, cancel_path, hold FROM rx_payments WHERE id = ?`, id).
		Scan(&ref, &status, &itemsJSON, &minor, &currency, &success, &cancel, &hold)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if r.Method == http.MethodPost {
		r.Body = http.MaxBytesReader(w, r.Body, 8<<10)
		if !p.validCSRF(r) {
			http.Error(w, "the form expired; reload the page", http.StatusForbidden)
			return
		}
		switch r.PostFormValue("decision") {
		case "pay":
			to := "paid"
			if hold {
				// Most cards hold an online authorisation for 7 days.
				to = "authorized"
				st.admin.ExecContext(r.Context(), `UPDATE rx_payments SET hold_expires_at = ? WHERE id = ? AND status = 'pending'`,
					time.Now().UTC().Add(7*24*time.Hour).Format("2006-01-02 15:04:05"), id)
			}
			if err := p.settle(r.Context(), st, id, to, "preview.buyer@example.com", "Preview buyer", ""); err != nil {
				http.Error(w, "could not record the payment", http.StatusInternalServerError)
				return
			}
			http.Redirect(w, r, success, http.StatusSeeOther)
		case "fail":
			p.settle(r.Context(), st, id, "failed", "", "", "")
			http.Redirect(w, r, cancel, http.StatusSeeOther)
		default:
			http.Redirect(w, r, cancel, http.StatusSeeOther)
		}
		return
	}
	var items []checkoutItem
	json.Unmarshal([]byte(itemsJSON), &items)
	data := map[string]any{"csrf": p.csrf(w, r), "id": id, "reference": ref, "status": status, "items": items, "hold": hold,
		"total": fmt.Sprintf("%.2f %s", float64(minor)/100, strings.ToUpper(currency))}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	simulatedCheckout.Execute(w, data)
}

var simulatedCheckout = template.Must(template.New("pay").Parse(`<!doctype html><html lang="en"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1"><title>Simulated checkout</title>
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
<p class="note">Preview: this is a simulated checkout. No money moves. On the live app, shoppers pay on Stripe's checkout page.</p>
{{if .hold}}<h1>Hold {{.total}} on your card</h1><p>The amount is authorised now and taken later, all or in part, or released.</p>{{else}}<h1>Pay {{.total}}</h1>{{end}}<p>Order {{.reference}}{{if ne .status "pending"}} · already {{.status}}{{end}}</p>
<table>{{range .items}}<tr><td>{{.Name}} × {{if .Quantity}}{{.Quantity}}{{else}}1{{end}}</td><td class="r">{{printf "%.2f" .Price}}</td></tr>{{end}}</table>
<form method="post"><input type="hidden" name="csrf" value="{{.csrf}}">
<button class="pay" name="decision" value="pay">{{if .hold}}Authorise (simulated){{else}}Pay (simulated){{end}}</button><button class="alt" name="decision" value="fail">Simulate a failed payment</button>
<button class="alt" name="decision" value="cancel">Cancel</button></form></body></html>`))

// stripeCheckoutPrefix is Stripe Checkout's address, where scripts may send
// shoppers to pay.
const stripeCheckoutPrefix = "https://checkout.stripe.com/"

// HandleStripeEvent applies a verified webhook event. It returns an error
// only for problems worth a retry by Stripe.
func (p *Platform) HandleStripeEvent(ctx context.Context, e stripe.Event) error {
	if p.cfg.SelfHosted && e.Account == "" {
		e.Account = stripe.DirectAccount // an event of the owner's own account
	}
	if dedupeEvent(e.Type) {
		// Events whose effect is not a plain status change are applied once
		// per event id; Stripe may deliver an event more than once.
		if p.seenStripeEvent(ctx, e.ID) {
			return nil
		}
		if err := p.handleMoneyEvent(ctx, e); err != nil {
			return err
		}
		p.markStripeEvent(ctx, e.ID)
		return nil
	}
	switch e.Type {
	case "account.updated":
		var a stripe.Account
		if err := json.Unmarshal(e.Data.Object, &a); err != nil {
			return nil
		}
		_, err := p.db.ExecContext(ctx, `UPDATE payment_accounts SET charges_enabled = ?, details_submitted = ?, updated_at = ? WHERE stripe_account = ?`,
			a.ChargesEnabled, a.DetailsSubmitted, now(), a.ID)
		return err
	case "charge.refunded":
		var c stripe.Charge
		if err := json.Unmarshal(e.Data.Object, &c); err != nil || c.PaymentIntent == "" {
			return nil
		}
		var app string
		if err := p.db.QueryRowContext(ctx, `SELECT app FROM payment_accounts WHERE stripe_account = ?`, e.Account).Scan(&app); err != nil {
			return nil // not an account of ours
		}
		st, err := p.loadStage(app, Live)
		if err != nil || st == nil {
			return err
		}
		return p.refund(ctx, st, c.PaymentIntent, c.AmountRefunded, c.Refunded)
	case "checkout.session.completed", "checkout.session.async_payment_succeeded", "checkout.session.async_payment_failed", "checkout.session.expired":
	default:
		return nil
	}
	var s stripe.Session
	if err := json.Unmarshal(e.Data.Object, &s); err != nil {
		return nil
	}
	app := p.currentName(s.Metadata["rx_app"]) // the app may have been renamed since
	if app == "" {
		return nil // not created by Renoxium
	}
	pa, err := p.PaymentAccount(app)
	if err != nil {
		return err
	}
	if pa.StripeAccount == "" || pa.StripeAccount != e.Account {
		return nil // the event's account is not this app's: ignore it
	}
	if s.Mode == "subscription" || s.Metadata["rx_subscription"] != "" {
		return p.subscriptionCheckoutEvent(ctx, app, pa.StripeAccount, e.Type, s)
	}
	if e.Type == "checkout.session.completed" {
		if handled, err := p.holdAuthorized(ctx, app, pa.StripeAccount, s); handled {
			return err
		}
	}
	status := ""
	switch {
	case e.Type == "checkout.session.completed" && s.PaymentStatus == "paid", e.Type == "checkout.session.async_payment_succeeded":
		status = "paid"
	case e.Type == "checkout.session.async_payment_failed":
		status = "failed"
	case e.Type == "checkout.session.expired":
		status = "expired"
	default:
		return nil // completed but not yet paid (delayed methods): wait for the async event
	}
	st, err := p.loadStage(app, Live)
	if err != nil {
		return err
	}
	if st == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	return p.settle(ctx, st, s.ID, status, s.Customer.Email, s.Customer.Name, s.PaymentIntent)
}

// SalesPeriod sums paid card payments in one period.
type SalesPeriod struct {
	Label  string
	Count  int
	Amount float64
}

// Sales summarises an app's paid live card payments, in the app's main
// currency (the one with the most payments). Cash on delivery is not seen
// by the platform.
func (p *Platform) Sales(ctx context.Context, app string) (currency string, periods []SalesPeriod, err error) {
	st, err := p.loadStage(app, p.AdminStage(app))
	if err != nil || st == nil {
		return "", nil, err
	}
	currency = "aed"
	st.admin.QueryRowContext(ctx, `SELECT currency FROM rx_payments WHERE status = 'paid' GROUP BY currency ORDER BY count(*) DESC LIMIT 1`).Scan(&currency)
	now := time.Now().UTC()
	for _, pr := range []struct {
		label string
		from  time.Time
	}{
		{"Today", time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)},
		{"This month", time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)},
		{"Last 30 days", now.AddDate(0, 0, -30)},
	} {
		sp := SalesPeriod{Label: pr.label}
		var minor int64
		err := st.admin.QueryRowContext(ctx, `SELECT count(*), coalesce(sum(amount), 0) FROM rx_payments
			WHERE status = 'paid' AND currency = ? AND coalesce(updated_at, created_at) >= ?`, currency, pr.from.Format("2006-01-02 15:04:05")).Scan(&sp.Count, &minor)
		if err != nil {
			return "", nil, err
		}
		sp.Amount = float64(minor) / 100
		periods = append(periods, sp)
	}
	return strings.ToUpper(currency), periods, nil
}
