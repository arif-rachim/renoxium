package platform_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/arif-rachim/renoxium/examples"
	"github.com/arif-rachim/renoxium/internal/appdef"
	"github.com/arif-rachim/renoxium/internal/platform"
)

// The UAE store example works end to end on preview: browsing in two
// languages, checkout by card (simulated) and cash on delivery, VAT,
// delivery fee, stock, and orders private to their buyer.
func TestStoreUAEExample(t *testing.T) {
	p := newPlatform(t)
	ctx := context.Background()
	ex, _ := examples.Get("store-uae")
	def, problems := appdef.Parse(ex.JSON)
	if len(problems) > 0 {
		t.Fatal(problems)
	}
	p.CreateApp("usr_test", "noor")
	res, err := p.ApplyChange(ctx, "noor", def)
	if err != nil || !res.OK {
		t.Fatal(err, res)
	}
	req := func(in platform.TestRequestInput) platform.TestResponse {
		t.Helper()
		r, err := p.TestRequest(ctx, "noor", in)
		if err != nil {
			t.Fatalf("%+v: %v", in, err)
		}
		if r.Status >= 500 {
			t.Fatalf("%s %s: %d\n%s", in.Method, in.Path, r.Status, logText(r))
		}
		return r
	}
	stock := func(key string) float64 {
		r := req(platform.TestRequestInput{Path: "/api/products?ids=" + key})
		var out struct{ Products []map[string]any }
		json.Unmarshal([]byte(r.Body), &out)
		return out.Products[0]["stock"].(float64)
	}

	home := req(platform.TestRequestInput{Path: "/"})
	if !strings.Contains(home.Body, "Royal Oud") || !strings.Contains(home.Body, "AED 250.00") || !strings.Contains(home.Body, `dir="ltr"`) {
		t.Fatalf("home: %s", home.Body)
	}
	ar := req(platform.TestRequestInput{Path: "/?lang=ar"})
	if !strings.Contains(ar.Body, `dir="rtl"`) || !strings.Contains(ar.Body, "العود الملكي") || !strings.Contains(ar.Body, "عطور النور") {
		t.Fatal("arabic home")
	}
	// Arabic pages show Arabic categories and prices in dirhams, and keep the brand theme from the store settings.
	if !strings.Contains(ar.Body, ">مسك</a>") || !strings.Contains(ar.Body, "95.00 د.إ") || !strings.Contains(ar.Body, `data-font="classic"`) ||
		!strings.Contains(ar.Body, "--brand:#5c2a3d") {
		t.Fatalf("arabic home details: %s", ar.Body)
	}
	if r := req(platform.TestRequestInput{Path: "/?c=Musk"}); strings.Contains(r.Body, "Royal Oud") || !strings.Contains(r.Body, "White Musk") {
		t.Fatal("category filter")
	}
	if r := req(platform.TestRequestInput{Path: "/p/1"}); r.Status != 200 || !strings.Contains(r.Body, "Includes VAT") ||
		!strings.Contains(r.Body, `type="radio" name="variant" value="2"`) || !strings.Contains(r.Body, "100 ml") {
		t.Fatalf("product with options: %d", r.Status)
	}
	if !strings.Contains(home.Body, "From AED 250.00") {
		t.Fatal("home does not show the lowest option price")
	}
	if r := req(platform.TestRequestInput{Path: "/api/products?ids=1,1.2"}); !strings.Contains(r.Body, `"key":"1.2"`) || strings.Contains(r.Body, `"key":"1"`) ||
		!strings.Contains(r.Body, `"priceNum":420`) {
		t.Fatalf("api with options: %s", r.Body)
	}
	if r := req(platform.TestRequestInput{Path: "/p/999"}); r.Status != 404 {
		t.Fatalf("missing product: %d", r.Status)
	}

	cart := `[{"id":1,"v":1,"qty":2,"price":1}]` // a price from the browser must be ignored
	form := func(payment, c string) map[string]string {
		return map[string]string{"cart": c, "name": "Fatima", "phone": "+971501234567", "address": "Villa 3, Al Barsha", "emirate": "Dubai", "payment": payment}
	}
	// Signed out: checkout sends the shopper to sign in and back to the cart.
	if r := req(platform.TestRequestInput{Method: "POST", Path: "/checkout", Form: form("card", cart)}); r.Status != 303 || !strings.HasPrefix(r.Location, "/_rx/auth/login?next=") {
		t.Fatalf("signed out checkout: %+v", r)
	}
	req(platform.TestRequestInput{Method: "POST", Path: "/_rx/auth/signup", Form: map[string]string{"name": "Fatima", "email": "fatima@mail.ae", "password": "fatima password"}})
	as := func(in platform.TestRequestInput) platform.TestResponse {
		in.UserEmail = "fatima@mail.ae"
		return req(in)
	}
	if r := as(platform.TestRequestInput{Path: "/cart"}); !strings.Contains(r.Body, `value="Fatima"`) || !strings.Contains(r.Body, "Cash on delivery") {
		t.Fatal("cart form for a signed-in shopper")
	}

	// Card: 2 x 250 = 500, free delivery over 200, VAT included = 500 * 5/105.
	r := as(platform.TestRequestInput{Method: "POST", Path: "/checkout", Form: form("card", cart)})
	if r.Status != 303 || !strings.HasPrefix(r.Location, "/_rx/pay/") {
		t.Fatalf("card checkout: %+v", r)
	}
	if !strings.Contains(req(platform.TestRequestInput{Path: r.Location}).Body, "500.00 AED") {
		t.Fatal("charged amount")
	}
	if stock("1.1") != 10 {
		t.Fatalf("option stock after order = %v", stock("1.1"))
	}
	paid := req(platform.TestRequestInput{Method: "POST", Path: r.Location, Form: map[string]string{"decision": "pay"}})
	if paid.Location != "/orders/1?placed=1" {
		t.Fatalf("after paying: %+v", paid)
	}
	order := as(platform.TestRequestInput{Path: "/orders/1?placed=1"})
	if !strings.Contains(order.Body, "<strong>Paid</strong>") || !strings.Contains(order.Body, "AED 500.00") || !strings.Contains(order.Body, "AED 23.81") ||
		!strings.Contains(order.Body, "Thank you") {
		t.Fatalf("order page: %s", order.Body)
	}

	// Cash on delivery: 95 + 15 delivery under the threshold.
	r = as(platform.TestRequestInput{Method: "POST", Path: "/checkout", Form: form("cod", `[{"id":2,"qty":1}]`)})
	if r.Location != "/orders/2?placed=1" {
		t.Fatalf("cod: %+v", r)
	}
	if o := as(platform.TestRequestInput{Path: "/orders/2"}); !strings.Contains(o.Body, "pay on delivery") || !strings.Contains(o.Body, "AED 110.00") {
		t.Fatalf("cod order: %s", o.Body)
	}

	// Preview never sends email: confirmations and the owner notice are in the log.
	logs, _ := p.Logs("noor", logFilterAll())
	for _, want := range []string{"not sent from preview: to fatima@mail.ae, \"Your order #1 at Al Noor Perfumes\"",
		"https://noor--preview.renoxium.test/orders/2", "to the owner, \"New order #2 (cash on delivery): AED 110.00\""} {
		if countMessages(logs, want) == 0 {
			t.Errorf("missing email in log: %s", want)
		}
	}

	// A failed card payment gives the stock back.
	r = as(platform.TestRequestInput{Method: "POST", Path: "/checkout", Form: form("card", `[{"id":3,"qty":5}]`)})
	if stock("3") != 25 {
		t.Fatalf("stock before failure = %v", stock("3"))
	}
	req(platform.TestRequestInput{Method: "POST", Path: r.Location, Form: map[string]string{"decision": "fail"}})
	if stock("3") != 30 {
		t.Fatalf("stock after failure = %v", stock("3"))
	}
	if o := as(platform.TestRequestInput{Path: "/orders/3"}); !strings.Contains(o.Body, "Payment failed") {
		t.Fatal("failed order status")
	}

	// An option's own price and stock: 420 for 100 ml, and the stock comes back after a failed payment.
	r = as(platform.TestRequestInput{Method: "POST", Path: "/checkout", Form: form("card", `[{"id":1,"v":2,"qty":1}]`)})
	if !strings.Contains(req(platform.TestRequestInput{Path: r.Location}).Body, "420.00 AED") || stock("1.2") != 7 {
		t.Fatalf("option checkout: %+v, stock %v", r, stock("1.2"))
	}
	req(platform.TestRequestInput{Method: "POST", Path: r.Location, Form: map[string]string{"decision": "fail"}})
	if stock("1.2") != 8 {
		t.Fatalf("option stock after failure = %v", stock("1.2"))
	}
	// A product with options needs one chosen, of that product.
	for _, c := range []string{`[{"id":1,"qty":1}]`, `[{"id":1,"v":99,"qty":1}]`} {
		if r := as(platform.TestRequestInput{Method: "POST", Path: "/checkout", Form: form("cod", c)}); r.Location != "/cart?error=error_cart" {
			t.Fatalf("cart %s: %+v", c, r)
		}
	}

	// The evening report counts today's orders and is sent to the owner (logged on preview).
	if r, _ := p.TestRequest(ctx, "noor", platform.TestRequestInput{Job: "daily_report"}); r.Status != 200 || r.Body != "4 orders reported" {
		t.Fatalf("daily report: %+v", r)
	}
	if logs, _ := p.Logs("noor", logFilterAll()); !anyLog(logs, `not sent from preview: to the owner, "Today: 4 orders, AED `) {
		t.Fatal("daily report not sent to the owner")
	}

	// Bad carts and missing fields go back to the cart with a message.
	if r := as(platform.TestRequestInput{Method: "POST", Path: "/checkout", Form: form("cod", `[{"id":4,"qty":11}]`)}); r.Location != "/cart?error=error_cart" {
		t.Fatalf("more than in stock: %+v", r)
	}
	f := form("cod", cart)
	f["address"] = ""
	if r := as(platform.TestRequestInput{Method: "POST", Path: "/checkout", Form: f}); r.Location != "/cart?error=error_fields" {
		t.Fatalf("missing address: %+v", r)
	}
	if r := req(platform.TestRequestInput{Path: "/cart?error=error_cart"}); !strings.Contains(r.Body, "no longer available") {
		t.Fatal("cart error message")
	}

	// Invoice for the paid order: 2 x 250 incl. VAT = 2 x 238.10 + VAT, totals 476.19 + 23.81.
	inv := as(platform.TestRequestInput{Path: "/orders/1/invoice"})
	for _, want := range []string{"<h1>Invoice</h1>", "Al Noor Perfumes Trading LLC", ">238.10<", ">23.81<", "AED 476.19", "AED 500.00", "Fatima"} {
		if !strings.Contains(inv.Body, want) {
			t.Errorf("invoice lacks %q", want)
		}
	}
	// A discount code: WELCOME10 takes 10% off products over AED 100.
	f = form("card", cart)
	f["coupon"] = "welcome10"
	r = as(platform.TestRequestInput{Method: "POST", Path: "/checkout", Form: f})
	if !strings.Contains(req(platform.TestRequestInput{Path: r.Location}).Body, "450.00 AED") {
		t.Fatal("discount not charged")
	}
	paidAgain := req(platform.TestRequestInput{Method: "POST", Path: r.Location, Form: map[string]string{"decision": "pay"}})
	id := strings.TrimSuffix(strings.TrimPrefix(paidAgain.Location, "/orders/"), "?placed=1")
	if o := as(platform.TestRequestInput{Path: "/orders/" + id}); !strings.Contains(o.Body, "Discount (WELCOME10)") || !strings.Contains(o.Body, "AED 450.00") {
		t.Fatalf("discounted order: %s", o.Body)
	}
	if inv := as(platform.TestRequestInput{Path: "/orders/" + id + "/invoice"}); !strings.Contains(inv.Body, "Discount (WELCOME10)") || !strings.Contains(inv.Body, "AED 428.57") {
		t.Fatalf("discounted invoice: %s", inv.Body)
	}
	f["coupon"] = "NOPE"
	if r := as(platform.TestRequestInput{Method: "POST", Path: "/checkout", Form: f}); r.Location != "/cart?error=error_coupon" {
		t.Fatalf("bad coupon: %+v", r)
	}

	// Orders are private: another shopper sees neither the list entry nor the page.
	req(platform.TestRequestInput{Method: "POST", Path: "/_rx/auth/signup", Form: map[string]string{"email": "other@mail.ae", "password": "other password"}})
	other := func(path string) platform.TestResponse {
		return req(platform.TestRequestInput{Path: path, UserEmail: "other@mail.ae"})
	}
	if r := other("/orders/1/invoice"); r.Status != 404 {
		t.Fatalf("someone else's invoice: %d", r.Status)
	}
	if r := other("/orders/1"); r.Status != 404 {
		t.Fatalf("someone else's order: %d", r.Status)
	}
	if r := other("/orders"); strings.Contains(r.Body, "Order #1<") || !strings.Contains(r.Body, "no orders yet") {
		t.Fatal("someone else's order listed")
	}
	if r := as(platform.TestRequestInput{Path: "/orders"}); !strings.Contains(r.Body, "Order #1<") || !strings.Contains(r.Body, "Order #2<") {
		t.Fatal("own orders list")
	}
}

func TestStoreUAETools(t *testing.T) {
	p := newPlatform(t)
	ctx := context.Background()
	ex, _ := examples.Get("store-uae")
	def, problems := appdef.Parse(ex.JSON)
	if len(problems) > 0 {
		t.Fatal(problems)
	}
	p.CreateApp("usr_test", "noor")
	if res, err := p.ApplyChange(ctx, "noor", def); err != nil || !res.OK {
		t.Fatal(err, res)
	}
	call := func(name string, args map[string]any) string {
		t.Helper()
		r, err := p.TestRequest(ctx, "noor", platform.TestRequestInput{Method: "POST", Path: "/mcp",
			JSON: map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{"name": name, "arguments": args}}})
		if err != nil || r.Status != 200 {
			t.Fatalf("%s: %v %+v", name, err, r)
		}
		var rpc struct {
			Result struct {
				IsError bool `json:"isError"`
				Content []struct{ Text string }
			}
		}
		json.Unmarshal([]byte(r.Body), &rpc)
		if rpc.Result.IsError || len(rpc.Result.Content) == 0 {
			t.Fatalf("%s failed: %s", name, r.Body)
		}
		return rpc.Result.Content[0].Text
	}
	if out := call("search_products", map[string]any{"query": "oud"}); !strings.Contains(out, `"name":"Royal Oud"`) || !strings.Contains(out, `"price_is_from":true`) ||
		!strings.Contains(out, `"url":"https://noor--preview.renoxium.test/p/1"`) {
		t.Errorf("search: %s", out)
	}
	if out := call("get_product", map[string]any{"id": 1, "lang": "ar"}); !strings.Contains(out, `"name":"100 مل","price":420`) {
		t.Errorf("product: %s", out)
	}
	if out := call("store_info", nil); !strings.Contains(out, `"payment_methods":["card","cash on delivery"]`) {
		t.Errorf("store info: %s", out)
	}
	if r, err := p.TestRequest(ctx, "noor", platform.TestRequestInput{Job: "daily_report"}); err != nil || r.Status != 200 || r.Body != "no orders today" {
		t.Errorf("daily report without orders: %v %+v", err, r)
	}
}
