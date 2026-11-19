package platform_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/arif-rachim/renoxium/examples"
	"github.com/arif-rachim/renoxium/internal/appdef"
	"github.com/arif-rachim/renoxium/internal/platform"
)

// With store.preorder_days set, the UAE store takes orders ahead: the shopper
// must choose a date far enough away, and the owner's evening report lists
// what to prepare for tomorrow.
func TestStoreUAEOrderAhead(t *testing.T) {
	p := newPlatform(t)
	ctx := context.Background()
	ex, _ := examples.Get("store-uae")
	def, problems := appdef.Parse(ex.JSON)
	if len(problems) > 0 {
		t.Fatal(problems)
	}
	def.Seed["store"][0]["preorder_days"] = 1
	p.CreateApp("usr_test", "cakes")
	if res, err := p.ApplyChange(ctx, "cakes", def); err != nil || !res.OK {
		t.Fatal(err, res)
	}
	req := func(in platform.TestRequestInput) platform.TestResponse {
		t.Helper()
		r, err := p.TestRequest(ctx, "cakes", in)
		if err != nil {
			t.Fatalf("%+v: %v", in, err)
		}
		if r.Status >= 500 {
			t.Fatalf("%s %s: %d\n%s", in.Method, in.Path, r.Status, logText(r))
		}
		return r
	}
	req(platform.TestRequestInput{Method: "POST", Path: "/_rx/auth/signup", Form: map[string]string{"name": "Mariam", "email": "mariam@mail.ae", "password": "mariam password"}})
	as := func(in platform.TestRequestInput) platform.TestResponse {
		in.UserEmail = "mariam@mail.ae"
		return req(in)
	}
	dubai := time.Now().UTC().Add(4 * time.Hour)
	today, tomorrow := dubai.Format("2006-01-02"), dubai.AddDate(0, 0, 1).Format("2006-01-02")

	if r := as(platform.TestRequestInput{Path: "/cart"}); !strings.Contains(r.Body, `name="wanted_date" type="date" required min="`+tomorrow+`"`) {
		t.Fatalf("cart has no date field from tomorrow: %s", r.Body)
	}
	order := func(date string) platform.TestResponse {
		return as(platform.TestRequestInput{Method: "POST", Path: "/checkout", Form: map[string]string{"cart": `[{"id":2,"qty":1}]`, "name": "Mariam",
			"phone": "+971501234567", "address": "Villa 3, Al Barsha", "emirate": "Dubai", "payment": "cod", "wanted_date": date, "note": "Happy birthday Noor"}})
	}
	for _, bad := range []string{"", today, "2020-01-01", "soon", "2031-02-31x"} {
		if r := order(bad); r.Location != "/cart?error=error_date" {
			t.Fatalf("date %q accepted: %+v", bad, r)
		}
	}
	if r := as(platform.TestRequestInput{Path: "/cart?error=error_date"}); !strings.Contains(r.Body, "Please choose a later date.") {
		t.Fatal("date error not shown")
	}
	r := order(tomorrow)
	if r.Location != "/orders/1?placed=1" {
		t.Fatalf("order ahead: %+v\n%s", r, logText(r))
	}
	if logs, _ := p.Logs("cakes", logFilterAll()); !anyLog(logs, "(cash on delivery): AED ") || !anyLog(logs, ", for "+tomorrow) {
		t.Fatal("owner notice has no date")
	}
	page := as(platform.TestRequestInput{Path: "/orders/1"})
	if !strings.Contains(page.Body, `<time datetime="`+tomorrow+`">`) || !strings.Contains(page.Body, "Happy birthday Noor") {
		t.Fatalf("order page: %s", page.Body)
	}
	job, err := p.TestRequest(ctx, "cakes", platform.TestRequestInput{Job: "daily_report"})
	if err != nil || job.Status != 200 || job.Body != "1 orders reported" {
		t.Fatalf("daily report: %v %+v", err, job)
	}
}
