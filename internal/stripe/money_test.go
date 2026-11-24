package stripe

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func TestRefundOnTheConnectedAccount(t *testing.T) {
	var got captured
	c := fake(t, 200, `{"id":"re_1","status":"succeeded","amount":2500}`, &got)
	r, err := c.CreateRefund(context.Background(), "acct_m", "pi_1", 2500, "requested_by_customer", true, map[string]string{"rx_by": "owner@x.ae"})
	if err != nil || r.ID != "re_1" {
		t.Fatal(r, err)
	}
	if got.path != "/v1/refunds" || got.account != "acct_m" || got.form.Get("payment_intent") != "pi_1" || got.form.Get("amount") != "2500" ||
		got.form.Get("reason") != "requested_by_customer" || got.form.Get("refund_application_fee") != "true" || got.form.Get("metadata[rx_by]") != "owner@x.ae" {
		t.Fatalf("%+v", got)
	}
	// Free text is not a Stripe reason; the whole rest is refunded without an amount.
	c.CreateRefund(context.Background(), "acct_m", "pi_1", 0, "late delivery", false, nil)
	if got.form.Has("reason") || got.form.Has("amount") || got.form.Has("refund_application_fee") {
		t.Fatalf("%+v", got.form)
	}
}

func TestHoldCaptureAndRelease(t *testing.T) {
	var got captured
	c := fake(t, 200, `{"id":"cs_1","url":"https://checkout.stripe.com/c/pay/cs_1"}`, &got)
	c.CreateCheckout(context.Background(), "acct_m", CheckoutParams{Currency: "aed", Items: []LineItem{{Name: "Deposit", Amount: 100000, Quantity: 1}}, Hold: true})
	if got.form.Get("payment_intent_data[capture_method]") != "manual" || got.form.Get("allowed_payment_method_types[0]") != "card" {
		t.Fatalf("%v", got.form)
	}
	c = fake(t, 200, `{"id":"pi_1","status":"succeeded","amount_received":30000}`, &got)
	if _, err := c.CapturePaymentIntent(context.Background(), "acct_m", "pi_1", 30000, 600); err != nil {
		t.Fatal(err)
	}
	if got.path != "/v1/payment_intents/pi_1/capture" || got.account != "acct_m" || got.form.Get("amount_to_capture") != "30000" || got.form.Get("application_fee_amount") != "600" {
		t.Fatalf("%+v", got)
	}
	c.CancelPaymentIntent(context.Background(), "acct_m", "pi_1")
	if got.method != "POST" || got.path != "/v1/payment_intents/pi_1/cancel" {
		t.Fatalf("%+v", got)
	}
	c = fake(t, 200, `{"id":"pi_1","status":"requires_capture","amount_capturable":100000,
		"latest_charge":{"id":"ch_1","payment_method_details":{"card":{"capture_before":1900000000}}}}`, &got)
	pi, err := c.GetPaymentIntent(context.Background(), "acct_m", "pi_1")
	if err != nil || pi.Status != "requires_capture" || !pi.CaptureBefore().Equal(time.Unix(1900000000, 0)) || got.form.Get("expand[]") != "latest_charge" {
		t.Fatal(pi, err, got)
	}
	// Not expanded: only the charge id, so no expiry.
	if (PaymentIntent{LatestCharge: json.RawMessage(`"ch_1"`)}).CaptureBefore() != (time.Time{}) {
		t.Fatal("expiry from an unexpanded charge")
	}
}

func TestSubscriptionCheckoutOnTheConnectedAccount(t *testing.T) {
	var got captured
	c := fake(t, 200, `{"id":"cs_s","url":"https://checkout.stripe.com/c/pay/cs_s"}`, &got)
	_, err := c.CreateSubscriptionCheckout(context.Background(), "acct_m", SubscriptionParams{Currency: "aed", Interval: "week", IntervalCount: 2,
		Items: []LineItem{{Name: "Cleaning", Amount: 20000, Quantity: 1}}, SuccessURL: "https://s/ok", CancelURL: "https://s/no", Reference: "c-1",
		Metadata: map[string]string{"rx_app": "clean"}, TrialDays: 7, FeePercent: 2})
	if err != nil {
		t.Fatal(err)
	}
	f := got.form
	if got.account != "acct_m" || f.Get("mode") != "subscription" || f.Get("line_items[0][price_data][recurring][interval]") != "week" ||
		f.Get("line_items[0][price_data][recurring][interval_count]") != "2" || f.Get("subscription_data[application_fee_percent]") != "2" ||
		f.Get("subscription_data[trial_period_days]") != "7" || f.Get("subscription_data[metadata][rx_app]") != "clean" || f.Get("metadata[rx_app]") != "clean" {
		t.Fatalf("%v", f)
	}
	for k := range f {
		if len(k) > 19 && k[:19] == "payment_intent_data" {
			t.Fatalf("payment_intent_data in subscription mode: %s", k)
		}
	}
}

func TestInvoiceShapes(t *testing.T) {
	var now, old Invoice
	json.Unmarshal([]byte(`{"id":"in_1","billing_reason":"subscription_cycle","parent":{"subscription_details":{"subscription":"sub_1","metadata":{"rx_app":"a"}}},
		"lines":{"data":[{"period":{"start":100,"end":200}}]},"period_start":1,"period_end":2}`), &now)
	json.Unmarshal([]byte(`{"id":"in_2","subscription":"sub_2","subscription_details":{"metadata":{"rx_app":"b"}},"payment_intent":"pi_2","period_start":1,"period_end":2}`), &old)
	if now.SubscriptionID() != "sub_1" || now.SubscriptionMetadata()["rx_app"] != "a" || old.SubscriptionID() != "sub_2" || old.SubscriptionMetadata()["rx_app"] != "b" {
		t.Fatal(now, old)
	}
	if s, e := now.ServicePeriod(); s.Unix() != 100 || e.Unix() != 200 {
		t.Fatal(s, e)
	}
	if s, e := old.ServicePeriod(); s.Unix() != 1 || e.Unix() != 2 {
		t.Fatal(s, e)
	}
	var sub Subscription
	json.Unmarshal([]byte(`{"id":"sub_1","current_period_end":300}`), &sub)
	if sub.PeriodEnd().Unix() != 300 {
		t.Fatal(sub.PeriodEnd())
	}
}
