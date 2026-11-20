package platform_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/arif-rachim/renoxium/examples"
	"github.com/arif-rachim/renoxium/internal/appdef"
	"github.com/arif-rachim/renoxium/internal/platform"
	"github.com/arif-rachim/renoxium/internal/sandbox"
)

// The store-uae delivery fee: free_delivery_over 0 or empty means no
// free-delivery threshold, so the flat fee is always charged; a threshold
// above 0 makes delivery free from that subtotal on. The cart (browser
// script) and the checkout (server script, which decides what the order is
// charged) follow the same rule.
func TestStoreUAEDeliveryFee(t *testing.T) {
	dir := t.TempDir()
	p, err := platform.New(platform.Config{DataDir: dir, AppsDomain: "renoxium.test", PublicScheme: "https"}, sandbox.NewGoja())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(p.Close)
	ctx := context.Background()
	ex, _ := examples.Get("store-uae")
	cases := []struct {
		name      string
		over      any    // store.free_delivery_over in the seed; nil = empty
		cart      string // White Musk is set to AED 90; Royal Oud 50 ml is AED 250
		sub, want float64
	}{
		{"zero", 0, `[{"id":2,"qty":2}]`, 180, 15},
		{"empty", nil, `[{"id":2,"qty":2}]`, 180, 15},
		{"over-200-above", 200, `[{"id":1,"v":1,"qty":1}]`, 250, 0},
		{"over-200-below", 200, `[{"id":2,"qty":2}]`, 180, 15},
	}
	for i, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			def, problems := appdef.Parse(ex.JSON)
			if len(problems) > 0 {
				t.Fatal(problems)
			}
			def.Seed["store"][0]["free_delivery_over"] = c.over
			def.Seed["store"][0]["delivery_fee"] = 15
			def.Seed["products"][1]["price"] = 90
			app := fmt.Sprintf("dlv%d", i)
			p.CreateApp("usr_test", app)
			if res, err := p.ApplyChange(ctx, app, def); err != nil || !res.OK {
				t.Fatal(err, res)
			}
			req := func(in platform.TestRequestInput) platform.TestResponse {
				t.Helper()
				r, err := p.TestRequest(ctx, app, in)
				if err != nil || r.Status >= 500 {
					t.Fatalf("%s %s: %v %d\n%s", in.Method, in.Path, err, r.Status, logText(r))
				}
				return r
			}

			req(platform.TestRequestInput{Method: "POST", Path: "/_rx/auth/signup", Form: map[string]string{"name": "Fatima", "email": "fatima@mail.ae", "password": "fatima password"}})

			// Cart: the page's own script, with the cart in the browser's storage, shows delivery and total.
			cartPage := req(platform.TestRequestInput{Path: "/cart", UserEmail: "fatima@mail.ae"}).Body
			vm := pageScript(t, p, app, "fatima@mail.ae", cartPage, "cart-field",
				`var __cart = `+c.cart+`; function rxCart() { return __cart; } function rxSave(c) { __cart = c; }`)
			wantDel := fmt.Sprintf("AED %.2f", c.want)
			if c.want == 0 {
				wantDel = "Free"
			}
			if got := text(t, vm, "delivery"); got != wantDel {
				t.Errorf("cart shows delivery %q, want %q", got, wantDel)
			}
			if got, want := text(t, vm, "total"), fmt.Sprintf("AED %.2f", c.sub+c.want); got != want {
				t.Errorf("cart shows total %q, want %q", got, want)
			}
			// The cart drawer (motion kit) works the totals out the same way.
			dvm := pageScript(t, p, app, "fatima@mail.ae", cartPage, "rxDrawer=",
				`var __cart = `+c.cart+`; function rxCart() { return __cart; } function rxSave(c) { __cart = c; }`)
			api := req(platform.TestRequestInput{Path: "/api/products?ids=" + cartKeys(c.cart)}).Body
			v, err := dvm.RunString(`(function(){ var s = window.rxDrawer.sum(__cart, ` + api + `); return [s.del, s.tot]; })()`)
			if err != nil {
				t.Fatal(err)
			}
			if got := v.Export().([]any); toFloat(got[0]) != c.want || toFloat(got[1]) != c.sub+c.want {
				t.Errorf("cart drawer shows delivery %v total %v, want %v and %v", got[0], got[1], c.want, c.sub+c.want)
			}

			// Checkout: the order row stores what the shopper is charged.
			r := req(platform.TestRequestInput{Method: "POST", Path: "/checkout", UserEmail: "fatima@mail.ae", Form: map[string]string{
				"cart": c.cart, "name": "Fatima", "phone": "+971501234567", "address": "Villa 3, Al Barsha", "emirate": "Dubai", "payment": "cod"}})
			if r.Status != 303 || r.Location != "/orders/1?placed=1" {
				t.Fatalf("checkout: %+v", r)
			}
			db, err := sql.Open("sqlite", "file:"+filepath.Join(dir, "apps", app, "preview.db")+"?mode=ro&_pragma=busy_timeout(5000)")
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			var sub, del, total float64
			if err := db.QueryRow(`SELECT subtotal, delivery, total FROM orders WHERE id = 1`).Scan(&sub, &del, &total); err != nil {
				t.Fatal(err)
			}
			if sub != c.sub || del != c.want || total != c.sub+c.want {
				t.Errorf("order row: subtotal %v delivery %v total %v, want %v + %v = %v", sub, del, total, c.sub, c.want, c.sub+c.want)
			}
		})
	}
}

// cartKeys turns a cart's JSON into the ids parameter of /api/products, e.g. "1.1,2".
func cartKeys(cart string) string {
	var items []struct{ ID, V int }
	json.Unmarshal([]byte(cart), &items)
	keys := []string{}
	for _, i := range items {
		if i.V > 0 {
			keys = append(keys, fmt.Sprintf("%d.%d", i.ID, i.V))
		} else {
			keys = append(keys, fmt.Sprint(i.ID))
		}
	}
	return strings.Join(keys, ",")
}

func toFloat(v any) float64 {
	switch x := v.(type) {
	case int64:
		return float64(x)
	case float64:
		return x
	}
	return -1
}
