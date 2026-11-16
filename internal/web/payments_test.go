package web_test

import (
	"bytes"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/arif-rachim/renoxium/internal/appdef"
	"github.com/arif-rachim/renoxium/internal/stripe"
)

func TestStripeWebhookNeedsAValidSignature(t *testing.T) {
	e := newEnv(t)
	payload := []byte(`{"id":"evt_1","type":"checkout.session.completed","account":"acct_x","data":{"object":{"id":"cs_1","metadata":{"rx_app":"nobody"}}}}`)
	post := func(sig string) int {
		req, _ := http.NewRequest("POST", e.base+"/stripe/webhook", bytes.NewReader(payload))
		req.Header.Set("Stripe-Signature", sig)
		r, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return r.StatusCode
	}
	if code := post(""); code != 400 {
		t.Fatalf("no signature: %d", code)
	}
	if code := post(stripe.Sign(payload, "whsec_wrong", time.Now())); code != 400 {
		t.Fatalf("wrong secret: %d", code)
	}
	if code := post(stripe.Sign(payload, "whsec_test", time.Now())); code != 204 {
		t.Fatalf("valid event: %d", code)
	}
}

func TestAdminPaymentsPage(t *testing.T) {
	e := newEnv(t)
	owner := e.browser()
	e.signup(owner, "rana@shop.ae")
	e.publishDef(owner, "rana-shop", &appdef.App{Templates: map[string]string{"t": "x"}, Routes: []appdef.Route{{Method: "GET", Path: "/", Template: "t"}}})
	r, _ := owner.Get(e.base + "/apps/rana-shop")
	if page := read(t, r); !strings.Contains(page, "/apps/rana-shop/payments") {
		t.Fatal("overview does not link to payments")
	}
	r, _ = owner.Get(e.base + "/apps/rana-shop/payments")
	if page := read(t, r); r.StatusCode != 200 || !strings.Contains(page, "not available on this Renoxium server") {
		t.Fatalf("payments page: %d %s", r.StatusCode, page)
	}
	other := e.browser()
	e.signup(other, "sami@shop.ae")
	if r, _ := other.Get(e.base + "/apps/rana-shop/payments"); r.StatusCode != 404 {
		t.Fatalf("other owner: %d", r.StatusCode)
	}
}
