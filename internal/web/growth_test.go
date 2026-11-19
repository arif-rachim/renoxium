package web_test

import (
	"encoding/csv"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/arif-rachim/renoxium/internal/accounts"
	"github.com/arif-rachim/renoxium/internal/appdef"
)

func TestOperatorGrowth(t *testing.T) {
	e := newEnv(t)
	e.site.Operators = []string{"ops@renoxium.ae"}

	// One owner goes all the way to a first customer order on live.
	merchant := e.browser()
	e.signup(merchant, "zara@shop.ae")
	http.Get(linkRE.FindString(e.mail.last(t, "zara@shop.ae", "verify_email").Text))
	e.publishDef(merchant, "zara-shop", &appdef.App{
		Scripts:   map[string]string{"order": `function handle(req) { notify.owner({subject: 'New order #1', text: 'x'}); return {text: 'ok'} }`},
		Templates: map[string]string{"t": "shop"},
		Routes:    []appdef.Route{{Method: "GET", Path: "/", Template: "t"}, {Method: "POST", Path: "/order", Script: "order"}}})
	w := httptest.NewRecorder()
	e.p.ServeApp(w, httptest.NewRequest("POST", "http://zara-shop.renoxium.test/order", nil))
	if w.Code != 200 {
		t.Fatalf("order: %d", w.Code)
	}
	// One only signs up; one pays for Pro without building anything yet.
	e.signup(e.browser(), "lead@shop.ae")
	e.signup(e.browser(), "payer@shop.ae")
	payer, _ := e.acc.UserByEmail("payer@shop.ae")
	e.acc.SetCustomer(payer.ID, "cus_payer")
	e.acc.SetSubscription(payer.ID, accounts.Billing{Plan: accounts.PlanPro, Status: "active", Subscription: "sub_1"})

	ops := e.browser()
	e.signup(ops, "ops@renoxium.ae")
	http.Get(linkRE.FindString(e.mail.last(t, "ops@renoxium.ae", "verify_email").Text))

	// Only the operator sees any of it.
	for _, c := range []*http.Client{merchant, e.browser()} {
		if r, _ := c.Get(e.base + "/operator/growth.csv"); r.StatusCode != 404 {
			t.Fatalf("csv for a non-operator: %d", r.StatusCode)
		}
		if r, _ := c.PostForm(e.base+"/operator/growth", url.Values{"csrf": {e.csrf(merchant, "/")}, "action": {"margin"}, "margin": {"50"}}); r.StatusCode != 404 {
			t.Fatalf("growth form for a non-operator: %d", r.StatusCode)
		}
	}

	r, _ := ops.Get(e.base + "/operator")
	page := read(t, r)
	for _, want := range []string{`id="growth"`, "3 owners, all time", "Signed up", "Email confirmed", "Agent connected", "First publish to live",
		"First customer activity", "Paid plan started", "Still paying after 3 months", "33%", // 1 of 3 sign-ups published
		"AED 99", "not enough data: fewer than 5 paying accounts", "enter marketing spend for the last three months", "aim for 3 or more",
		"Activation by template", "<td>custom</td>", "Download funnel CSV", "Last 12 weeks"} {
		if !strings.Contains(page, want) {
			t.Errorf("growth section lacks %q", want)
		}
	}
	if strings.Contains(page, "4 owners") {
		t.Error("the operator is counted as an owner")
	}
	month := time.Now().UTC().Format("2006-01")
	if r, _ := ops.Get(e.base + "/operator?cohort=" + month); !strings.Contains(read(t, r), "3 owners, signed up in "+time.Now().UTC().Format("January 2006")) {
		t.Error("this month's cohort")
	}
	if r, _ := ops.Get(e.base + "/operator?cohort=2020-01"); !strings.Contains(read(t, r), "0 owners, signed up in January 2020") {
		t.Error("an empty cohort")
	}

	// Marketing spend and margin: CSRF and validation.
	csrf := e.csrf(ops, "/operator")
	post := func(form url.Values) int {
		form.Set("csrf", csrf)
		r, _ := ops.PostForm(e.base+"/operator/growth", form)
		read(t, r)
		return r.StatusCode
	}
	if r, _ := ops.PostForm(e.base+"/operator/growth", url.Values{"action": {"spend"}, "month": {month}, "spend": {"100"}}); r.StatusCode != 403 {
		t.Fatalf("spend without csrf: %d", r.StatusCode)
	}
	next := time.Now().UTC().AddDate(0, 1, 0).Format("2006-01")
	for _, bad := range []url.Values{
		{"action": {"spend"}, "month": {"2026-13"}, "spend": {"100"}},
		{"action": {"spend"}, "month": {next}, "spend": {"100"}},
		{"action": {"spend"}, "month": {month}, "spend": {"12.50"}},
		{"action": {"spend"}, "month": {month}, "spend": {"-5"}},
		{"action": {"margin"}, "margin": {"0"}},
		{"action": {"margin"}, "margin": {"abc"}},
		{"action": {"other"}},
	} {
		if code := post(bad); code != 400 {
			t.Errorf("%v: %d", bad, code)
		}
	}
	if code := post(url.Values{"action": {"spend"}, "month": {month}, "spend": {"1,500"}}); code != 303 {
		t.Fatalf("spend: %d", code)
	}
	if code := post(url.Values{"action": {"margin"}, "margin": {"70%"}}); code != 303 {
		t.Fatalf("margin: %d", code)
	}
	r, _ = ops.Get(e.base + "/operator")
	page = read(t, r)
	// The payer started this month: CAC = 1500 ÷ 1.
	for _, want := range []string{"<td class=\"n\">AED 1500</td>", `value="70"`, "AED 1500</div>"} {
		if !strings.Contains(page, want) {
			t.Errorf("after saving, page lacks %q", want)
		}
	}

	// The per-owner CSV.
	r, _ = ops.Get(e.base + "/operator/growth.csv")
	if r.StatusCode != 200 || !strings.HasPrefix(r.Header.Get("Content-Type"), "text/csv") || !strings.Contains(r.Header.Get("Content-Disposition"), "attachment") {
		t.Fatalf("csv: %d %v", r.StatusCode, r.Header)
	}
	rows, err := csv.NewReader(strings.NewReader(read(t, r))).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 4 || rows[0][0] != "owner_id" || rows[0][11] != "retained_m3" {
		t.Fatalf("csv rows %v", rows)
	}
	byEmail := map[string][]string{}
	for _, row := range rows[1:] {
		byEmail[row[1]] = row
	}
	z := byEmail["zara@shop.ae"]
	if z == nil || z[2] != "free" || z[3] != "custom" && z[3] != "" {
		t.Fatalf("zara row %v", z)
	}
	for i := 4; i <= 9; i++ { // sign-up to first customer activity
		if z[i] == "" {
			t.Errorf("zara lacks %s", rows[0][i])
		}
	}
	if p := byEmail["payer@shop.ae"]; p == nil || p[2] != "pro" || p[10] == "" || p[7] != "" {
		t.Errorf("payer row %v", p)
	}
	if _, ok := byEmail["ops@renoxium.ae"]; ok {
		t.Error("operator in the csv")
	}
	if r, _ := ops.Get(e.base + "/operator/growth.csv?cohort=2020-01"); len(strings.Split(strings.TrimSpace(read(t, r)), "\n")) != 1 {
		t.Error("cohort csv")
	}
}
