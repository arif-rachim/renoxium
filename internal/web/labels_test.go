package web_test

import (
	"net/url"
	"strings"
	"testing"

	"github.com/arif-rachim/renoxium/internal/appdef"
)

func TestAdminLabelsAndOptions(t *testing.T) {
	e := newEnv(t)
	owner := e.browser()
	e.signup(owner, "huda@shop.ae")
	e.publishDef(owner, "huda-shop", &appdef.App{
		Tables: []appdef.Table{{Name: "orders", Label: "Customer orders", Columns: []appdef.Column{
			{Name: "customer_name", Type: "text", Label: "Who ordered"},
			{Name: "status", Type: "text", Options: []string{"pending", "shipped"}, Help: "Change to shipped when it leaves the shop."}}}},
		Seed:      map[string][]map[string]any{"orders": {{"customer_name": "Aisha", "status": "pending"}}},
		Templates: map[string]string{"t": "shop"}, Routes: []appdef.Route{{Method: "GET", Path: "/", Template: "t"}}})
	get := func(path string) string {
		r, _ := owner.Get(e.base + path)
		return read(t, r)
	}
	if page := get("/apps/huda-shop"); !strings.Contains(page, "Customer orders") {
		t.Fatal("overview misses the table label")
	}
	if page := get("/apps/huda-shop/t/orders"); !strings.Contains(page, "Who ordered") {
		t.Fatal("list misses the column label")
	}
	page := get("/apps/huda-shop/t/orders/1")
	for _, want := range []string{"Who ordered", `<select id="f-status" name="status">`, "<option selected>pending</option>", "<option>shipped</option>", "leaves the shop"} {
		if !strings.Contains(page, want) {
			t.Fatalf("form misses %q:\n%s", want, page)
		}
	}
	csrf := e.csrf(owner, "/apps/huda-shop/t/orders/1")
	r, _ := owner.PostForm(e.base+"/apps/huda-shop/t/orders/1", url.Values{"csrf": {csrf}, "customer_name": {"Aisha"}, "status": {"lost"}})
	if body := read(t, r); r.StatusCode != 422 || !strings.Contains(body, "choose one of: pending, shipped") {
		t.Fatalf("unknown option: %d", r.StatusCode)
	}
	r, _ = owner.PostForm(e.base+"/apps/huda-shop/t/orders/1", url.Values{"csrf": {csrf}, "customer_name": {"Aisha"}, "status": {"shipped"}})
	if r.StatusCode != 303 {
		t.Fatalf("valid option: %d", r.StatusCode)
	}
}
