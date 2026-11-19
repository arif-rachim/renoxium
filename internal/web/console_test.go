package web_test

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// The owner's console: a dashboard with charts, and lists that page, sort
// and choose their columns in the database, shown as cards on a phone.
func TestConsoleListsAndDashboard(t *testing.T) {
	e := newEnv(t)
	alice := e.browser()
	e.signup(alice, "alice@shop.ae")
	e.publishShop(alice, "al-noor")
	for i := 0; i < 120; i++ {
		if _, err := e.p.AdminSave(context.Background(), "al-noor", "orders", 0, map[string]string{"customer_name": fmt.Sprintf("Customer %03d", i)}, "alice@shop.ae"); err != nil {
			t.Fatal(err)
		}
	}
	get := func(path string) (int, string, *http.Response) {
		t.Helper()
		r, err := alice.Get(e.base + path)
		if err != nil {
			t.Fatal(err)
		}
		return r.StatusCode, read(t, r), r
	}

	code, page, _ := get("/apps/al-noor")
	for _, want := range []string{"Overview", "Visitors per day", "Card sales per day", "Where visitors come from", `href="/apps/al-noor/t/orders"`, `aria-current="page"`, "Payments"} {
		if code != 200 || !strings.Contains(page, want) {
			t.Fatalf("dashboard lacks %q (%d)", want, code)
		}
	}
	if _, page, _ := get("/apps/al-noor?days=7"); !strings.Contains(page, `href="?days=7" aria-current="true"`) {
		t.Fatal("period 7 days not selected")
	}

	// Pages of 50 by default, numbered; sizes and sorting in the address.
	_, page, _ = get("/apps/al-noor/t/orders")
	if !strings.Contains(page, "1–50 of 121") || strings.Count(page, `<td class="id"`) != 50 || !strings.Contains(page, `data-label="Customer name"`) {
		t.Fatalf("first page: %s", page)
	}
	_, page, _ = get("/apps/al-noor/t/orders?page=3&size=25")
	if !strings.Contains(page, "51–75 of 121") || strings.Count(page, `<td class="id"`) != 25 {
		t.Fatal("page 3 of 25")
	}
	_, page, _ = get("/apps/al-noor/t/orders?sort=customer_name&dir=asc&size=25")
	if i, j := strings.Index(page, "Customer 000"), strings.Index(page, "Customer 001"); i < 0 || j < i {
		t.Fatal("not sorted by customer name, ascending")
	}
	// The column picker keeps the choice in one cols= parameter.
	noRedirect := *alice
	noRedirect.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	r, err := noRedirect.Get(e.base + "/apps/al-noor/t/products?c=name&c=stock")
	if err != nil || r.StatusCode != http.StatusSeeOther || r.Header.Get("Location") != "/apps/al-noor/t/products?cols=name%2Cstock" {
		t.Fatalf("column picker: %v %d %s", err, r.StatusCode, r.Header.Get("Location"))
	}
	_, page, _ = get("/apps/al-noor/t/products?cols=name,stock")
	if !strings.Contains(page, `data-label="Stock"`) || strings.Contains(page, `data-label="Price aed"`) {
		t.Fatal("chosen columns not shown")
	}
	// A column that does not exist is ignored, not passed to the database.
	if code, _, _ := get("/apps/al-noor/t/products?sort=nope&cols=nope"); code != 200 {
		t.Fatalf("unknown column: %d", code)
	}
}
