package platform_test

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/dop251/goja"

	"github.com/arif-rachim/renoxium/examples"
	"github.com/arif-rachim/renoxium/internal/appdef"
	"github.com/arif-rachim/renoxium/internal/platform"
	"github.com/arif-rachim/renoxium/internal/sandbox"
)

// cartDeliveryRE finds the cart page's delivery rule in its inline script.
var cartDeliveryRE = regexp.MustCompile(`var del=([^;]+?),tot=sub\+del;`)

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

			// Cart: run the page's own delivery rule with what /api/products gives it.
			m := cartDeliveryRE.FindStringSubmatch(req(platform.TestRequestInput{Path: "/cart"}).Body)
			if m == nil {
				t.Fatal("no delivery rule in the cart script")
			}
			vm := goja.New()
			vm.Set("d", map[string]any{"free_delivery_over": c.over, "delivery_fee": 15.0})
			vm.Set("sub", c.sub)
			v, err := vm.RunString(m[1])
			if err != nil {
				t.Fatalf("cart rule %q: %v", m[1], err)
			}
			if got := v.ToFloat(); got != c.want {
				t.Errorf("cart delivery for %v with threshold %v = %v, want %v (rule %s)", c.sub, c.over, got, c.want, m[1])
			}
			if c.over == nil { // an empty field reaches the browser as "" or null
				vm.Set("d", map[string]any{"free_delivery_over": "", "delivery_fee": 15.0})
				if v, _ := vm.RunString(m[1]); v.ToFloat() != c.want {
					t.Errorf(`cart delivery with "" threshold = %v, want %v`, v.ToFloat(), c.want)
				}
			}

			// Checkout: the order row stores what the shopper is charged.
			req(platform.TestRequestInput{Method: "POST", Path: "/_rx/auth/signup", Form: map[string]string{"name": "Fatima", "email": "fatima@mail.ae", "password": "fatima password"}})
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
