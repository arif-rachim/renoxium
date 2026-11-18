package platform_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"testing"

	"github.com/arif-rachim/renoxium/internal/appdef"
	"github.com/arif-rachim/renoxium/internal/platform"
	"github.com/arif-rachim/renoxium/internal/sandbox"
	"github.com/arif-rachim/renoxium/internal/stripe"
)

// moneyShop takes payments, card holds and subscriptions, and logs every
// on_payment event as "on_payment <event> <reference> <amount>".
func moneyShop() *appdef.App {
	return &appdef.App{
		Tables: []appdef.Table{{Name: "orders", Columns: []appdef.Column{{Name: "status", Type: "text"}}}},
		Scripts: map[string]string{
			"buy": `function handle(req) {
				const o = db.exec("INSERT INTO orders (status) VALUES ('awaiting payment')");
				const pay = payments.checkout({items: [{name: "Car rental deposit", price: 250, quantity: 2}], reference: "order-" + o.lastInsertId,
					hold: req.form.hold === "1", success_path: "/", cancel_path: "/"});
				return {redirect: pay.url};
			}`,
			"refund": `function handle(req) {
				return {json: payments.refund({reference: "order-" + req.params.id, amount: req.form.amount ? Number(req.form.amount) : undefined, reason: req.form.reason})};
			}`,
			"capture": `function handle(req) {
				return {json: payments.capture({reference: "order-" + req.params.id, amount: req.form.amount ? Number(req.form.amount) : undefined})};
			}`,
			"release": `function handle(req) { return {json: payments.release({reference: "order-" + req.params.id})} }`,
			"status":  `function handle(req) { return {json: payments.get("order-" + req.params.id)} }`,
			"subscribe": `function handle(req) {
				const s = payments.subscribe({items: [{name: "Weekly cleaning", price: 200}], interval: req.form.interval || "week",
					interval_count: Number(req.form.count || 0), reference: "contract-1", end_after: Number(req.form.end_after || 0),
					trial_days: Number(req.form.trial || 0), success_path: "/thanks", cancel_path: "/", customer_email: "client@mail.ae"});
				return {redirect: s.url};
			}`,
			"sub":    `function handle(req) { return {json: payments.subscription("contract-1")} }`,
			"cancel": `function handle(req) { return {json: payments.cancel_subscription({reference: "contract-1", at_period_end: req.form.end === "1"})} }`,
			"paid": `function handle(req) {
				const b = req.body;
				if (b.reference.startsWith("order-")) db.exec("UPDATE orders SET status = ? WHERE id = ?", b.event, Number(b.reference.split("-")[1]));
				console.log("on_payment", b.event, b.reference, b.amount, b.subscription ? b.subscription.status : "");
				return {text: "ok"};
			}`,
		},
		OnPayment: "paid",
		Routes: []appdef.Route{
			{Method: "POST", Path: "/buy", Script: "buy"},
			{Method: "POST", Path: "/refund/:id", Script: "refund", Auth: "user"},
			{Method: "POST", Path: "/capture/:id", Script: "capture", Auth: "user"},
			{Method: "POST", Path: "/release/:id", Script: "release", Auth: "user"},
			{Method: "GET", Path: "/status/:id", Script: "status"},
			{Method: "POST", Path: "/subscribe", Script: "subscribe"},
			{Method: "GET", Path: "/sub", Script: "sub"},
			{Method: "POST", Path: "/cancel", Script: "cancel", Auth: "user"},
		},
	}
}

func hookCount(t *testing.T, p *platform.Platform, app, text string) int {
	t.Helper()
	logs, _ := p.Logs(app, logFilterAll())
	return countMessages(logs, "console: on_payment "+text)
}

func jsonOf(t *testing.T, body string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(body), &m); err != nil {
		t.Fatalf("not JSON: %s", body)
	}
	return m
}

func TestPreviewRefunds(t *testing.T) {
	p := newPlatform(t)
	ctx := context.Background()
	p.CreateApp("usr_test", "rent")
	if res, err := p.ApplyChange(ctx, "rent", moneyShop()); err != nil || !res.OK {
		t.Fatal(err, res)
	}
	buyer := previewBuyer(t, p, "rent")
	tr := func(method, path string, form map[string]string) platform.TestResponse {
		r, err := p.TestRequest(ctx, "rent", platform.TestRequestInput{Method: method, Path: path, Form: form, UserEmail: buyer})
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	// Nothing paid yet: nothing to refund.
	buy := tr("POST", "/buy", nil)
	if r := tr("POST", "/refund/1", nil); r.Status != 500 || !strings.Contains(logText(r), "no paid payment") {
		t.Fatalf("refund before paying: %+v", r)
	}
	tr("POST", buy.Location, map[string]string{"decision": "pay"})

	r := tr("POST", "/refund/1", map[string]string{"amount": "100", "reason": "late delivery"})
	if out := jsonOf(t, r.Body); r.Status != 200 || out["status"] != "partially_refunded" || out["refunded_amount"] != 100.0 || out["amount"] != 500.0 {
		t.Fatalf("partial refund: %d %s", r.Status, r.Body)
	}
	// The hook runs once, after the refunding script has finished.
	if n := hookCount(t, p, "rent", "partially_refunded order-1 500"); n != 1 {
		t.Fatalf("partially_refunded hook ran %d times", n)
	}
	// Never more than what is left, never more than 2 decimals.
	if r := tr("POST", "/refund/1", map[string]string{"amount": "400.01"}); r.Status != 500 || !strings.Contains(logText(r), "at most 400.00 AED") {
		t.Fatalf("over-refund: %+v", r)
	}
	if r := tr("POST", "/refund/1", map[string]string{"amount": "1.005"}); r.Status != 500 || !strings.Contains(logText(r), "at most 2 decimals") {
		t.Fatalf("3 decimals: %+v", r)
	}
	// Without an amount, the rest.
	r = tr("POST", "/refund/1", nil)
	if out := jsonOf(t, r.Body); out["status"] != "refunded" || out["refunded_amount"] != 500.0 {
		t.Fatalf("full refund: %s", r.Body)
	}
	if hookCount(t, p, "rent", "refunded order-1") != 1 {
		t.Fatal("refunded hook did not run once")
	}
	if r := tr("POST", "/refund/1", nil); r.Status != 500 {
		t.Fatalf("refund after a full refund: %+v", r)
	}
	logs, _ := p.Logs("rent", logFilterAll())
	if countMessages(logs, "a script refunded AED 100.00 of the payment for order-1 (late delivery)") != 1 {
		t.Fatal("refund not logged")
	}
}

func TestPreviewCardHold(t *testing.T) {
	p := newPlatform(t)
	ctx := context.Background()
	p.CreateApp("usr_test", "cars")
	p.ApplyChange(ctx, "cars", moneyShop())
	buyer := previewBuyer(t, p, "cars")
	tr := func(method, path string, form map[string]string) platform.TestResponse {
		r, _ := p.TestRequest(ctx, "cars", platform.TestRequestInput{Method: method, Path: path, Form: form, UserEmail: buyer})
		return r
	}
	buy := tr("POST", "/buy", map[string]string{"hold": "1"})
	if page := tr("GET", buy.Location, nil); !strings.Contains(page.Body, "Authorise (simulated)") || !strings.Contains(page.Body, "Hold 500.00 AED") {
		t.Fatalf("hold page: %s", page.Body)
	}
	tr("POST", buy.Location, map[string]string{"decision": "pay"})
	if hookCount(t, p, "cars", "authorized order-1 500") != 1 {
		t.Fatal("authorized hook missing")
	}
	got := jsonOf(t, tr("GET", "/status/1", nil).Body)
	if got["status"] != "authorized" || got["hold"] != true || got["authorized_amount"] != 500.0 || got["hold_expires_at"] == "" {
		t.Fatalf("payments.get of a hold: %v", got)
	}
	// A hold cannot be refunded; capture takes part of it (a damage charge).
	if r := tr("POST", "/refund/1", nil); r.Status != 500 {
		t.Fatal("refunded a hold")
	}
	if r := tr("POST", "/capture/1", map[string]string{"amount": "600"}); r.Status != 500 || !strings.Contains(logText(r), "at most the 500.00 AED held") {
		t.Fatalf("capture over the hold: %+v", r)
	}
	r := tr("POST", "/capture/1", map[string]string{"amount": "120.5"})
	if out := jsonOf(t, r.Body); out["status"] != "paid" || out["amount"] != 120.5 || out["authorized_amount"] != 500.0 {
		t.Fatalf("capture: %s", r.Body)
	}
	if hookCount(t, p, "cars", "paid order-1 120.5") != 1 {
		t.Fatal("paid hook after capture missing")
	}
	if r := tr("POST", "/capture/1", nil); r.Status != 500 {
		t.Fatal("captured twice")
	}
	// The captured amount can be refunded, at most that amount.
	if r := tr("POST", "/refund/1", map[string]string{"amount": "121"}); r.Status != 500 {
		t.Fatal("refunded more than captured")
	}

	// Another hold, released: nothing taken.
	buy = tr("POST", "/buy", map[string]string{"hold": "1"})
	tr("POST", buy.Location, map[string]string{"decision": "pay"})
	r = tr("POST", "/release/2", nil)
	if out := jsonOf(t, r.Body); out["status"] != "released" {
		t.Fatalf("release: %s", r.Body)
	}
	if hookCount(t, p, "cars", "released order-2 500") != 1 {
		t.Fatal("released hook missing")
	}
	if r := tr("POST", "/release/2", nil); r.Status != 500 {
		t.Fatal("released twice")
	}
}

func TestPreviewSubscription(t *testing.T) {
	p := newPlatform(t)
	ctx := context.Background()
	p.CreateApp("usr_test", "clean")
	p.ApplyChange(ctx, "clean", moneyShop())
	buyer := previewBuyer(t, p, "clean")
	tr := func(method, path string, form map[string]string) platform.TestResponse {
		r, _ := p.TestRequest(ctx, "clean", platform.TestRequestInput{Method: method, Path: path, Form: form, UserEmail: buyer})
		return r
	}
	if r := tr("POST", "/subscribe", map[string]string{"interval": "day"}); r.Status != 500 || !strings.Contains(logText(r), `interval must be "week"`) {
		t.Fatalf("bad interval: %+v", r)
	}
	if r := tr("POST", "/subscribe", map[string]string{"count": "53", "interval": "year"}); r.Status != 500 || !strings.Contains(logText(r), "between 1 and 3") {
		t.Fatalf("bad count: %+v", r)
	}
	sub := tr("POST", "/subscribe", map[string]string{"end_after": "3"})
	if sub.Status != 303 || !strings.HasPrefix(sub.Location, "/_rx/pay/rxs_") {
		t.Fatalf("subscribe: %+v", sub)
	}
	if page := tr("GET", sub.Location, nil); !strings.Contains(page.Body, "200.00 AED every week") || !strings.Contains(page.Body, "Subscribe (simulated)") {
		t.Fatalf("page: %s", page.Body)
	}
	if r := tr("POST", sub.Location, map[string]string{"decision": "renew"}); r.Status != 409 {
		t.Fatalf("renew before subscribing: %d", r.Status)
	}
	if r := tr("POST", sub.Location, map[string]string{"decision": "pay"}); r.Status != 303 || r.Location != "/thanks" {
		t.Fatalf("pay: %+v", r)
	}
	if hookCount(t, p, "clean", "paid contract-1 200 active") != 1 {
		t.Fatal("first payment hook missing")
	}
	s := jsonOf(t, tr("GET", "/sub", nil).Body)
	if s["status"] != "active" || s["payments_made"] != 1.0 || s["current_period_end"] == "" || s["customer_email"] != "client@mail.ae" {
		t.Fatalf("payments.subscription: %v", s)
	}
	// Renewals, simulated from test_request.
	tr("POST", sub.Location, map[string]string{"decision": "renew"})
	tr("POST", sub.Location, map[string]string{"decision": "renewal_failed"})
	if hookCount(t, p, "clean", "renewal_failed contract-1 200 past_due") != 1 {
		t.Fatal("renewal_failed hook missing")
	}
	tr("POST", sub.Location, map[string]string{"decision": "renew"})
	if n := hookCount(t, p, "clean", "renewed contract-1 200"); n != 2 {
		t.Fatalf("renewed ran %d times", n)
	}
	// end_after 3: the third payment ends it at the period's end.
	s = jsonOf(t, tr("GET", "/sub", nil).Body)
	if s["payments_made"] != 3.0 || s["cancel_at_period_end"] != true || s["status"] != "active" {
		t.Fatalf("after 3 payments: %v", s)
	}
	tr("POST", sub.Location, map[string]string{"decision": "renew"})
	if hookCount(t, p, "clean", "subscription_cancelled contract-1 200 canceled") != 1 {
		t.Fatal("subscription_cancelled hook missing")
	}
	if n := hookCount(t, p, "clean", "renewed"); n != 2 {
		t.Fatalf("renewed after the end: %d", n)
	}

	// Cancelled by a script, at once.
	sub = tr("POST", "/subscribe", map[string]string{"trial": "14"})
	tr("POST", sub.Location, map[string]string{"decision": "pay"})
	if hookCount(t, p, "clean", "paid contract-1 0 trialing") != 1 {
		t.Fatal("trial start hook missing")
	}
	r := tr("POST", "/cancel", map[string]string{"end": "1"})
	if out := jsonOf(t, r.Body); out["cancel_at_period_end"] != true || out["status"] != "trialing" {
		t.Fatalf("cancel at period end: %s", r.Body)
	}
	r = tr("POST", "/cancel", nil)
	if out := jsonOf(t, r.Body); out["status"] != "canceled" {
		t.Fatalf("cancel now: %s", r.Body)
	}
	if hookCount(t, p, "clean", "subscription_cancelled contract-1 200 canceled") != 2 {
		t.Fatal("cancel hook did not run")
	}
	if r := tr("POST", "/cancel", nil); r.Status != 500 || !strings.Contains(logText(r), "no running subscription") {
		t.Fatalf("cancel again: %+v", r)
	}
}

// liveMoneyShop publishes moneyShop for a Free-plan owner with Stripe
// connected to the fake.
func liveMoneyShop(t *testing.T, plan string) (*platform.Platform, *fakeStripe, string) {
	t.Helper()
	fs := &fakeStripe{charges: true, intents: map[string]map[string]any{}}
	srv := fs.server(t)
	p, err := platform.New(platform.Config{DataDir: t.TempDir(), AppsDomain: "renoxium.test", PublicScheme: "https",
		Stripe: &stripe.Client{Key: "sk_test_x", BaseURL: srv.URL}, OwnerPlan: func(string) string { return plan }}, sandbox.NewGoja())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { p.Close() })
	ctx := context.Background()
	p.CreateApp("usr_"+plan, "rent")
	p.ApplyChange(ctx, "rent", moneyShop())
	p.Publish("rent", 0)
	p.StripeOnboardingURL(ctx, "rent", "o@x.ae", "https://r", "https://f")
	if pa, err := p.RefreshPaymentAccount(ctx, "rent"); err != nil || !pa.ChargesEnabled {
		t.Fatal(pa, err)
	}
	return p, fs, "rent.renoxium.test"
}

func stripeEvent(t *testing.T, id, typ, account string, obj any) stripe.Event {
	raw, _ := json.Marshal(obj)
	e := stripe.Event{ID: id, Type: typ, Account: account}
	e.Data.Object = raw
	return e
}

func TestLiveRefundsAndHoldsThroughStripe(t *testing.T) {
	p, fs, host := liveMoneyShop(t, "free")
	buyer := liveBuyer(t, p, host)
	ctx := context.Background()
	apply := func(e stripe.Event) {
		t.Helper()
		if err := p.HandleStripeEvent(ctx, e); err != nil {
			t.Fatal(err)
		}
	}
	session := func(id, pi, payStatus string) map[string]any {
		return map[string]any{"id": id, "payment_status": payStatus, "payment_intent": pi, "metadata": map[string]string{"rx_app": "rent"},
			"customer_details": map[string]string{"email": "buyer@mail.ae", "name": "Buyer"}}
	}

	// A paid order, refunded in part by a script: a refund on the merchant's account, fee returned.
	do(t, p, "POST", host, "/buy", url.Values{})
	apply(stripeEvent(t, "evt_1", "checkout.session.completed", "acct_merchant", session("cs_test_1", "pi_1", "paid")))
	r := as(buyer, "POST", "/refund/1", url.Values{"amount": {"100"}, "reason": {"requested_by_customer"}})
	if r.status != 200 || !strings.Contains(r.body, `"status":"partially_refunded"`) {
		t.Fatalf("refund: %d %s", r.status, r.body)
	}
	refunds := fs.callsTo("POST", "/v1/refunds")
	if len(refunds) != 1 || refunds[0].account != "acct_merchant" || refunds[0].form.Get("payment_intent") != "pi_1" ||
		refunds[0].form.Get("amount") != "10000" || refunds[0].form.Get("refund_application_fee") != "true" ||
		refunds[0].form.Get("reason") != "requested_by_customer" || refunds[0].form.Get("metadata[rx_app]") != "rent" {
		t.Fatalf("refund call: %+v", refunds)
	}
	// Stripe's charge.refunded for the same amount does not run on_payment again.
	apply(stripeEvent(t, "evt_2", "charge.refunded", "acct_merchant", map[string]any{"id": "ch_1", "payment_intent": "pi_1", "amount": 50000, "amount_refunded": 10000}))
	if n := hookCount(t, p, "rent", "partially_refunded order-1"); n != 1 {
		t.Fatalf("partially_refunded ran %d times", n)
	}
	// More than is left is refused before Stripe is called.
	if r := as(buyer, "POST", "/refund/1", url.Values{"amount": {"450"}}); r.status != 500 || len(fs.callsTo("POST", "/v1/refunds")) != 1 {
		t.Fatalf("over-refund: %d", r.status)
	}
	// A Dashboard refund of the rest still reaches the order.
	apply(stripeEvent(t, "evt_3", "charge.refunded", "acct_merchant", map[string]any{"id": "ch_1", "payment_intent": "pi_1", "amount": 50000, "amount_refunded": 50000, "refunded": true}))
	if hookCount(t, p, "rent", "refunded order-1") != 1 {
		t.Fatal("dashboard refund not applied")
	}

	// A card hold: authorise, then capture part of it with the fee on what is taken.
	do(t, p, "POST", host, "/buy", url.Values{"hold": {"1"}})
	s := fs.sessions[len(fs.sessions)-1]
	if s.Get("payment_intent_data[capture_method]") != "manual" || s.Get("payment_method_types[0]") != "card" || s.Get("payment_intent_data[application_fee_amount]") != "1000" {
		t.Fatalf("hold session: %v", s)
	}
	fs.intents["pi_hold"] = map[string]any{"id": "pi_hold", "status": "requires_capture", "amount": 50000, "amount_capturable": 50000,
		"latest_charge": map[string]any{"id": "ch_h", "payment_method_details": map[string]any{"card": map[string]any{"capture_before": 1900000000}}}}
	// Checkout reports a hold as completed but unpaid; the PaymentIntent says it is authorised.
	apply(stripeEvent(t, "evt_4", "checkout.session.completed", "acct_merchant", session("cs_test_2", "pi_hold", "unpaid")))
	if hookCount(t, p, "rent", "authorized order-2 500") != 1 {
		t.Fatal("authorized hook missing")
	}
	if st := do(t, p, "GET", host, "/status/2", nil); !strings.Contains(st.body, `"hold_expires_at":"2030-03-17 17:46:40"`) {
		t.Fatalf("hold expiry: %s", st.body)
	}
	r = as(buyer, "POST", "/capture/2", url.Values{"amount": {"300"}})
	if r.status != 200 || !strings.Contains(r.body, `"status":"paid"`) || !strings.Contains(r.body, `"amount":300`) {
		t.Fatalf("capture: %d %s", r.status, r.body)
	}
	captures := fs.callsTo("POST", "/v1/payment_intents/pi_hold/capture")
	if len(captures) != 1 || captures[0].form.Get("amount_to_capture") != "30000" || captures[0].form.Get("application_fee_amount") != "600" ||
		captures[0].account != "acct_merchant" {
		t.Fatalf("capture call: %+v", captures)
	}
	// Stripe's payment_intent.succeeded afterwards changes nothing.
	apply(stripeEvent(t, "evt_5", "payment_intent.succeeded", "acct_merchant", map[string]any{"id": "pi_hold", "status": "succeeded", "amount_received": 30000}))
	if n := hookCount(t, p, "rent", "paid order-2"); n != 1 {
		t.Fatalf("paid after capture ran %d times", n)
	}

	// A hold that expires (Stripe cancels the PaymentIntent) is released.
	do(t, p, "POST", host, "/buy", url.Values{"hold": {"1"}})
	fs.intents["pi_hold3"] = map[string]any{"id": "pi_hold3", "status": "requires_capture", "amount_capturable": 50000}
	apply(stripeEvent(t, "evt_6", "checkout.session.completed", "acct_merchant", session("cs_test_3", "pi_hold3", "unpaid")))
	cancelled := stripeEvent(t, "evt_7", "payment_intent.canceled", "acct_merchant", map[string]any{"id": "pi_hold3", "status": "canceled"})
	apply(cancelled)
	apply(cancelled) // repeated delivery
	if hookCount(t, p, "rent", "released order-3") != 1 {
		t.Fatal("expired hold not released once")
	}
	// An event from another account is ignored.
	do(t, p, "POST", host, "/buy", url.Values{"hold": {"1"}})
	fs.intents["pi_hold4"] = map[string]any{"id": "pi_hold4", "status": "requires_capture", "amount_capturable": 50000}
	apply(stripeEvent(t, "evt_8", "checkout.session.completed", "acct_merchant", session("cs_test_4", "pi_hold4", "unpaid")))
	apply(stripeEvent(t, "evt_9", "payment_intent.canceled", "acct_someone_else", map[string]any{"id": "pi_hold4"}))
	if hookCount(t, p, "rent", "released order-4") != 0 {
		t.Fatal("event from another account applied")
	}
	// Releasing calls Stripe's cancel.
	if r := as(buyer, "POST", "/release/4", url.Values{}); r.status != 200 || len(fs.callsTo("POST", "/v1/payment_intents/pi_hold4/cancel")) != 1 {
		t.Fatalf("release: %d %s", r.status, r.body)
	}
}

func TestLiveSubscriptionThroughStripe(t *testing.T) {
	p, fs, host := liveMoneyShop(t, "free")
	buyer := liveBuyer(t, p, host)
	ctx := context.Background()
	apply := func(e stripe.Event) {
		t.Helper()
		if err := p.HandleStripeEvent(ctx, e); err != nil {
			t.Fatal(err)
		}
	}
	r := do(t, p, "POST", host, "/subscribe", url.Values{"interval": {"month"}, "count": {"3"}, "trial": {"7"}})
	if r.status != 303 || r.header.Get("Location") != "https://checkout.stripe.com/c/pay/cs_sub_1" {
		t.Fatalf("subscribe: %d %s", r.status, r.header.Get("Location"))
	}
	s := fs.sessions[len(fs.sessions)-1]
	if fs.headers[len(fs.headers)-1] != "acct_merchant" || s.Get("mode") != "subscription" ||
		s.Get("line_items[0][price_data][recurring][interval]") != "month" || s.Get("line_items[0][price_data][recurring][interval_count]") != "3" ||
		s.Get("line_items[0][price_data][unit_amount]") != "20000" || s.Get("subscription_data[application_fee_percent]") != "2" ||
		s.Get("subscription_data[trial_period_days]") != "7" || s.Get("subscription_data[metadata][rx_app]") != "rent" ||
		s.Get("payment_intent_data[application_fee_amount]") != "" || s.Get("customer_email") != "client@mail.ae" {
		t.Fatalf("subscription session: %v", s)
	}
	rxs := s.Get("subscription_data[metadata][rx_subscription]")
	if !strings.HasPrefix(rxs, "rxs_") {
		t.Fatalf("rx_subscription = %q", rxs)
	}
	meta := map[string]string{"rx_app": "rent", "rx_reference": "contract-1", "rx_subscription": rxs}
	invoice := func(id, reason string, amount int64) map[string]any {
		return map[string]any{"id": id, "billing_reason": reason, "amount_paid": amount, "amount_due": amount, "currency": "aed",
			"parent":         map[string]any{"subscription_details": map[string]any{"subscription": "sub_1", "metadata": meta}},
			"lines":          map[string]any{"data": []map[string]any{{"period": map[string]any{"start": 1800000000, "end": 1807776000}}}},
			"customer_email": "client@mail.ae"}
	}
	// The first invoice can arrive before the session's completion.
	apply(stripeEvent(t, "evt_in1", "invoice.paid", "acct_merchant", invoice("in_1", "subscription_create", 20000)))
	first := stripeEvent(t, "evt_cs", "checkout.session.completed", "acct_merchant", map[string]any{"id": "cs_sub_1", "mode": "subscription",
		"subscription": "sub_1", "metadata": meta, "customer_details": map[string]string{"email": "client@mail.ae", "name": "Client"}})
	apply(first)
	apply(stripeEvent(t, "evt_in1", "invoice.paid", "acct_merchant", invoice("in_1", "subscription_create", 20000))) // repeated
	if n := hookCount(t, p, "rent", "paid contract-1 200 active"); n != 1 {
		t.Fatalf("first payment hook ran %d times", n)
	}
	got := jsonOf(t, do(t, p, "GET", host, "/sub", nil).body)
	if got["status"] != "active" || got["current_period_end"] != "2030-03-17 17:46:40" || got["payments_made"] != 1.0 || got["customer_name"] != "Client" {
		t.Fatalf("subscription: %v", got)
	}
	// A renewal, a failure (with Stripe's retries), a paid retry.
	apply(stripeEvent(t, "evt_in2", "invoice.paid", "acct_merchant", invoice("in_2", "subscription_cycle", 20000)))
	apply(stripeEvent(t, "evt_in3a", "invoice.payment_failed", "acct_merchant", invoice("in_3", "subscription_cycle", 20000)))
	apply(stripeEvent(t, "evt_in3b", "invoice.payment_failed", "acct_merchant", invoice("in_3", "subscription_cycle", 20000)))
	apply(stripeEvent(t, "evt_in3c", "invoice.paid", "acct_merchant", invoice("in_3", "subscription_cycle", 20000)))
	if a, b := hookCount(t, p, "rent", "renewed contract-1 200"), hookCount(t, p, "rent", "renewal_failed contract-1 200"); a != 2 || b != 1 {
		t.Fatalf("renewed %d, renewal_failed %d", a, b)
	}
	// Renewal payments can be refunded; their PaymentIntent comes from invoice_payments.
	pm, err := p.AdminRefund(ctx, "rent", "in_2", "50", "owner@x.ae")
	if err != nil || pm.Status != "partially_refunded" || pm.Refunded != 50 {
		t.Fatalf("refund of a renewal: %v %+v", err, pm)
	}
	if c := fs.callsTo("POST", "/v1/refunds"); len(c) != 1 || c[0].form.Get("payment_intent") != "pi_for_in_2" || c[0].form.Get("amount") != "5000" ||
		c[0].form.Get("refund_application_fee") != "true" {
		t.Fatalf("renewal refund call: %+v", c)
	}
	if _, err := p.AdminRefund(ctx, "rent", "in_2", "150.01", "owner@x.ae"); err == nil || !strings.Contains(err.Error(), "at most 150.00 AED") {
		t.Fatalf("over-refund of a renewal: %v", err)
	}
	if _, err := p.AdminRefund(ctx, "rent", "in_nope", "", "owner@x.ae"); !errors.Is(err, platform.ErrNotFound) {
		t.Fatalf("unknown payment: %v", err)
	}
	// Cancel at the end of the period, then Stripe ends it.
	r = as(buyer, "POST", "/cancel", url.Values{"end": {"1"}})
	if r.status != 200 || !strings.Contains(r.body, `"cancel_at_period_end":true`) {
		t.Fatalf("cancel: %d %s", r.status, r.body)
	}
	if c := fs.callsTo("POST", "/v1/subscriptions/sub_1"); len(c) != 1 || c[0].form.Get("cancel_at_period_end") != "true" || c[0].account != "acct_merchant" {
		t.Fatalf("cancel call: %+v", c)
	}
	deleted := stripeEvent(t, "evt_del", "customer.subscription.deleted", "acct_merchant", map[string]any{"id": "sub_1", "status": "canceled", "metadata": meta})
	apply(deleted)
	apply(stripeEvent(t, "evt_del2", "customer.subscription.deleted", "acct_merchant", map[string]any{"id": "sub_1", "status": "canceled", "metadata": meta}))
	if n := hookCount(t, p, "rent", "subscription_cancelled contract-1 200 canceled"); n != 1 {
		t.Fatalf("subscription_cancelled ran %d times", n)
	}
	// An update arriving late cannot revive it.
	apply(stripeEvent(t, "evt_upd", "customer.subscription.updated", "acct_merchant", map[string]any{"id": "sub_1", "status": "active", "metadata": meta}))
	if got := jsonOf(t, do(t, p, "GET", host, "/sub", nil).body); got["status"] != "canceled" {
		t.Fatalf("revived: %v", got)
	}
	// Invoices of another account's subscription are ignored.
	apply(stripeEvent(t, "evt_x", "invoice.paid", "acct_someone_else", invoice("in_9", "subscription_cycle", 20000)))
	list, _ := p.Payments(ctx, "rent", 20)
	paid := 0
	for _, pm := range list {
		if pm.Subscription == rxs && (pm.Status == "paid" || pm.Status == "partially_refunded") {
			paid++
		}
	}
	if paid != 3 {
		t.Fatalf("%d paid subscription payments: %+v", paid, list)
	}
	subs, _ := p.Subscriptions(ctx, "rent", 10)
	if len(subs) != 1 || subs[0].Status != "canceled" || subs[0].PaymentsMade != 3 {
		t.Fatalf("admin list: %+v", subs)
	}
}

func TestSubscriptionFeeFollowsThePlan(t *testing.T) {
	p, fs, host := liveMoneyShop(t, "pro")
	buyer := liveBuyer(t, p, host)
	do(t, p, "POST", host, "/subscribe", url.Values{})
	s := fs.sessions[len(fs.sessions)-1]
	if s.Get("mode") != "subscription" || s.Get("subscription_data[application_fee_percent]") != "" {
		t.Fatalf("pro plan subscription: %v", s)
	}
	// Refunds of payments without a platform fee do not ask Stripe to refund one.
	do(t, p, "POST", host, "/buy", url.Values{})
	p.HandleStripeEvent(context.Background(), stripeEvent(t, "evt_p", "checkout.session.completed", "acct_merchant",
		map[string]any{"id": "cs_test_2", "payment_status": "paid", "payment_intent": "pi_p", "metadata": map[string]string{"rx_app": "rent"}}))
	if r := as(buyer, "POST", "/refund/1", url.Values{}); r.status != 200 {
		t.Fatalf("refund: %d %s", r.status, r.body)
	}
	if c := fs.callsTo("POST", "/v1/refunds"); len(c) != 1 || c[0].form.Get("refund_application_fee") != "" || c[0].form.Get("amount") != "50000" {
		t.Fatalf("refund call: %+v", c)
	}
}

func TestMoneyActionsAreRateLimited(t *testing.T) {
	p := newPlatform(t)
	ctx := context.Background()
	p.CreateApp("usr_test", "loop")
	p.ApplyChange(ctx, "loop", moneyShop())
	buyer := previewBuyer(t, p, "loop")
	tr := func(method, path string, form map[string]string) platform.TestResponse {
		r, _ := p.TestRequest(ctx, "loop", platform.TestRequestInput{Method: method, Path: path, Form: form, UserEmail: buyer})
		return r
	}
	buy := tr("POST", "/buy", nil)
	tr("POST", buy.Location, map[string]string{"decision": "pay"})
	for i := 0; i < platform.MoneyActionsPerHour; i++ {
		if r := tr("POST", "/refund/1", map[string]string{"amount": "0.01"}); r.Status != 200 {
			t.Fatalf("refund %d: %+v", i, r)
		}
	}
	if r := tr("POST", "/refund/1", map[string]string{"amount": "0.01"}); r.Status != 500 || !strings.Contains(logText(r), "in an hour") {
		t.Fatalf("past the limit: %+v", r)
	}
}

// A refund made by on_admin_change runs on_payment too, as from any script.
func TestAdminHookRefundRunsOnPayment(t *testing.T) {
	p := newPlatform(t)
	ctx := context.Background()
	p.CreateApp("usr_test", "rent")
	def := moneyShop()
	def.Scripts["admin"] = `function handle(req) {
		if (req.body.row && req.body.row.status === 'cancelled') payments.refund({reference: 'order-' + req.body.id});
		return {text: 'ok'};
	}`
	def.OnAdminChange = "admin"
	if res, err := p.ApplyChange(ctx, "rent", def); err != nil || !res.OK {
		t.Fatal(err, res)
	}
	buy, _ := p.TestRequest(ctx, "rent", platform.TestRequestInput{Method: "POST", Path: "/buy"})
	p.TestRequest(ctx, "rent", platform.TestRequestInput{Method: "POST", Path: buy.Location, Form: map[string]string{"decision": "pay"}})
	r, err := p.TestRequest(ctx, "rent", platform.TestRequestInput{Admin: &platform.TestAdminInput{Action: "update", Table: "orders", ID: 1,
		Values: map[string]any{"status": "cancelled"}}})
	if err != nil || r.Status != 200 {
		t.Fatalf("admin change: %v %+v", err, r)
	}
	if n := hookCount(t, p, "rent", "refunded order-1 500"); n != 1 {
		t.Fatalf("on_payment after an admin-hook refund ran %d times", n)
	}
}

// previewBuyer signs a visitor up on an app's preview and returns their
// email, for test requests made as them: money actions run only in
// requests of a signed-in visitor.
func previewBuyer(t *testing.T, p *platform.Platform, app string) string {
	t.Helper()
	const addr = "buyer@shop.ae"
	r, err := p.TestRequest(context.Background(), app, platform.TestRequestInput{Method: "POST", Path: "/_rx/auth/signup",
		Form: map[string]string{"email": addr, "password": "buyer's password"}})
	if err != nil || r.Status != 303 {
		t.Fatalf("buyer sign-up: %v %+v", err, r)
	}
	return addr
}

// liveBuyer is a signed-in visitor of a live app.
func liveBuyer(t *testing.T, p *platform.Platform, host string) *visitorBrowser {
	t.Helper()
	v := newVisitor(t, p, host)
	if w := v.submit("/_rx/auth/signup", "/_rx/auth/signup", url.Values{"email": {"buyer@shop.ae"}, "password": {"buyer's password"}}); w.Code != 303 {
		t.Fatalf("buyer sign-up: %d", w.Code)
	}
	return v
}

// as sends a request to the app's host as a signed-in visitor.
func as(v *visitorBrowser, method, path string, form url.Values) response {
	w := v.do(method, path, form)
	return response{status: w.Code, body: w.Body.String(), header: w.Header()}
}

// Refunds and the other money actions move a merchant's money, so they do
// not run where anyone on the internet can start a script without signing
// in: public MCP tools and requests without a signed-in visitor.
func TestMoneyActionsNeedASignedInVisitor(t *testing.T) {
	p := newPlatform(t)
	ctx := context.Background()
	p.CreateApp("usr_test", "rent")
	def := moneyShop()
	def.Routes = append(def.Routes, appdef.Route{Method: "POST", Path: "/open-refund/:id", Script: "refund"}) // no "auth": a script mistake
	def.Tools = []appdef.Tool{{Name: "refund_order", Description: "Refund an order", Script: "tool_refund",
		Params: []appdef.ToolParam{{Name: "id", Type: "string", Required: true}}}}
	def.Scripts["tool_refund"] = `function handle(req) { return {json: payments.refund({reference: "order-" + req.body.id})} }`
	if res, err := p.ApplyChange(ctx, "rent", def); err != nil || !res.OK {
		t.Fatal(err, res)
	}
	buyer := previewBuyer(t, p, "rent")
	tr := func(in platform.TestRequestInput) platform.TestResponse {
		r, err := p.TestRequest(ctx, "rent", in)
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	buy := tr(platform.TestRequestInput{Method: "POST", Path: "/buy"})
	tr(platform.TestRequestInput{Method: "POST", Path: buy.Location, Form: map[string]string{"decision": "pay"}})

	if r := tr(platform.TestRequestInput{Method: "POST", Path: "/open-refund/1"}); r.Status != 500 || !strings.Contains(logText(r), "only work in a request of a signed-in visitor") {
		t.Fatalf("refund from an anonymous request: %+v", r)
	}
	r := tr(platform.TestRequestInput{Method: "POST", Path: "/mcp", JSON: map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call",
		"params": map[string]any{"name": "refund_order", "arguments": map[string]any{"id": "1"}}}})
	if !strings.Contains(r.Body, `"isError":true`) {
		t.Fatalf("refund from a public MCP tool: %s", r.Body)
	}
	if st := tr(platform.TestRequestInput{Path: "/status/1"}); !strings.Contains(st.Body, `"status":"paid"`) {
		t.Fatalf("the payment changed: %s", st.Body)
	}
	// The same script in a signed-in visitor's request works.
	if r := tr(platform.TestRequestInput{Method: "POST", Path: "/open-refund/1", UserEmail: buyer}); r.Status != 200 || !strings.Contains(r.Body, `"status":"refunded"`) {
		t.Fatalf("refund by a signed-in visitor: %+v", r)
	}
}
