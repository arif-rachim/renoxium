package platform_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"testing"

	"github.com/arif-rachim/renoxium/examples"
	"github.com/arif-rachim/renoxium/internal/appdef"
	"github.com/arif-rachim/renoxium/internal/applog"
	"github.com/arif-rachim/renoxium/internal/platform"
)

// The quote portal example works end to end. The owner's part (writing
// quotes, marking bank transfers paid, posting updates) is done with the row
// functions behind the admin panel, which work on live data and run the
// app's on_admin_change script (it emails the client at once); the client's
// part runs on live for bank transfers and on preview, a copy of live data,
// for simulated card payments, admin changes made with test_request and jobs.
func TestQuotePortalUAEExample(t *testing.T) {
	p := newPlatform(t)
	ctx := context.Background()
	const app, host = "bayan", "bayan.renoxium.test"
	ex, ok := examples.Get("quote-portal-uae")
	if !ok {
		t.Fatal("example is not registered")
	}
	def, problems := appdef.Parse(ex.JSON)
	if len(problems) > 0 {
		t.Fatal(problems)
	}
	p.CreateApp("usr_test", app)
	apply := func() {
		t.Helper()
		res, err := p.ApplyChange(ctx, app, def)
		if err != nil || !res.OK {
			t.Fatal(err, res)
		}
	}
	apply()

	// Preview requests.
	req := func(in platform.TestRequestInput) platform.TestResponse {
		t.Helper()
		r, err := p.TestRequest(ctx, app, in)
		if err != nil {
			t.Fatalf("%+v: %v", in, err)
		}
		if r.Status >= 500 {
			t.Fatalf("%s %s: %d\n%s", in.Method, in.Path, r.Status, logText(r))
		}
		return r
	}
	as := func(email, method, path string, form map[string]string) platform.TestResponse {
		t.Helper()
		return req(platform.TestRequestInput{Method: method, Path: path, Form: form, UserEmail: email})
	}
	has := func(what, body string, wants ...string) {
		t.Helper()
		for _, want := range wants {
			if !strings.Contains(body, want) {
				t.Errorf("%s lacks %q", what, want)
			}
		}
	}
	hasNot := func(what, body string, bad ...string) {
		t.Helper()
		for _, b := range bad {
			if strings.Contains(body, b) {
				t.Errorf("%s contains %q", what, b)
			}
		}
	}

	// Emails are never sent from preview: they are written to the log, newest first.
	// This test server has no mail service, so live logs the emails it would send.
	stageMails := func(stage string) string {
		t.Helper()
		events, err := p.Logs(app, applog.Filter{Stage: stage, Source: "email", Limit: 200})
		if err != nil {
			t.Fatal(err)
		}
		var b strings.Builder
		for _, e := range events {
			b.WriteString(e.Message + "\n")
		}
		return b.String()
	}
	mails := func() string { t.Helper(); return stageMails("preview") }
	liveMails := func() string { t.Helper(); return stageMails("live") }

	// The public pages, in two languages.
	home := req(platform.TestRequestInput{Path: "/"})
	has("home", home.Body, `dir="ltr"`, "Bayan Business Setup", "Mainland LLC company setup", "From AED 14500.00", "Tax and accounting", "How it works")
	has("arabic home", req(platform.TestRequestInput{Path: "/?lang=ar"}).Body, `dir="rtl"`, "بيان لتأسيس الأعمال", "تأسيس الشركات", "ابتداءً من AED 12900.00", `href="/packages/1?lang=ar"`)
	pkg := req(platform.TestRequestInput{Path: "/packages/1"})
	has("package", pkg.Body, "What is included", "DET trade licence", "plus 5% VAT", `href="/_rx/auth/login?next=%2fpackages%2f1"`)
	hasNot("package for a guest", pkg.Body, `action="/enquiry"`)
	if r := req(platform.TestRequestInput{Path: "/packages/99"}); r.Status != 404 {
		t.Fatalf("missing package: %d", r.Status)
	}
	// Enquiries and the portal need an account.
	if r := req(platform.TestRequestInput{Path: "/enquiry"}); r.Status != 303 || !strings.HasPrefix(r.Location, "/_rx/auth/login?next=") {
		t.Fatalf("signed out enquiry form: %+v", r)
	}
	if r := req(platform.TestRequestInput{Method: "POST", Path: "/enquiry", Form: map[string]string{"name": "x", "phone": "1", "message": "x"}}); r.Status != 401 {
		t.Fatalf("signed out enquiry: %d", r.Status)
	}
	if r := req(platform.TestRequestInput{Path: "/quotes/1"}); r.Status != 303 {
		t.Fatalf("signed out quote: %d", r.Status)
	}

	// ---- Live: the client sends an enquiry, the owner answers in the admin panel.
	if _, err := p.Publish(app, 0); err != nil {
		t.Fatal(err)
	}
	signup := func(name, email string) *visitorBrowser {
		t.Helper()
		v := newVisitor(t, p, host)
		if w := v.submit("/_rx/auth/signup", "/_rx/auth/signup", url.Values{"name": {name}, "email": {email}, "password": {name + " password"}}); w.Code != 303 {
			t.Fatalf("signup %s: %d %s", email, w.Code, w.Body.String())
		}
		return v
	}
	fatima, omar := signup("Fatima Al Marri", "fatima@mail.ae"), signup("Omar", "omar@mail.ae")
	get := func(v *visitorBrowser, path string) (int, string) {
		t.Helper()
		w := v.do("GET", path, nil)
		if w.Code >= 500 {
			t.Fatalf("GET %s: %d %s", path, w.Code, w.Body.String())
		}
		return w.Code, w.Body.String()
	}
	post := func(v *visitorBrowser, path string, form url.Values) (int, string) {
		t.Helper()
		if form == nil {
			form = url.Values{}
		}
		w := v.do("POST", path, form)
		if w.Code >= 500 {
			t.Fatalf("POST %s: %d %s", path, w.Code, w.Body.String())
		}
		return w.Code, w.Header().Get("Location")
	}
	page := func(v *visitorBrowser, path string) string {
		t.Helper()
		code, body := get(v, path)
		if code != 200 {
			t.Fatalf("GET %s: %d", path, code)
		}
		return body
	}

	enquiry := url.Values{"package_id": {"1"}, "name": {"Fatima Al Marri"}, "company": {"Al Reem Trading"}, "email": {"fatima@mail.ae"},
		"message": {"Two partners, general trading, three visas."}, "target_date": {"January"}}
	// A missing phone brings the form back with what was typed.
	if w := fatima.do("POST", "/enquiry", enquiry); w.Code != 422 || !strings.Contains(w.Body.String(), "Please enter your name, your phone") ||
		!strings.Contains(w.Body.String(), "Two partners, general trading") {
		t.Fatalf("enquiry without phone: %d", w.Code)
	}
	enquiry.Set("phone", "+971501234567")
	if code, loc := post(fatima, "/enquiry", enquiry); code != 303 || loc != "/portal?done=done_enquiry" {
		t.Fatalf("enquiry: %d %s", code, loc)
	}
	has("portal after the enquiry", page(fatima, "/portal?done=done_enquiry"), "We received your enquiry", "Mainland LLC company setup", "We are preparing your quote")

	// The owner, as in the admin panel: add a row, or load a row, change some fields and save it.
	add := func(table string, values map[string]string) int64 {
		t.Helper()
		id, err := p.AdminSave(ctx, app, table, 0, values, "owner@bayan.ae")
		if err != nil {
			t.Fatalf("add to %s: %v", table, err)
		}
		return id
	}
	row := func(table string, id int64) map[string]string {
		t.Helper()
		r, err := p.AdminGet(ctx, app, table, id)
		if err != nil {
			t.Fatalf("%s #%d: %v", table, id, err)
		}
		out := map[string]string{}
		for k, v := range r {
			if v != nil {
				out[k] = fmt.Sprint(v)
			}
		}
		return out
	}
	edit := func(table string, id int64, change map[string]string) {
		t.Helper()
		values := row(table, id)
		for k, v := range change {
			values[k] = v
		}
		if _, err := p.AdminSave(ctx, app, table, id, values, "owner@bayan.ae"); err != nil {
			t.Fatalf("edit %s #%d: %v", table, id, err)
		}
	}
	quote := func(title, status, lines string, more map[string]string) int64 {
		t.Helper()
		values := map[string]string{"enquiry_id": "1", "title": title, "status": status, "lines": lines}
		for k, v := range more {
			values[k] = v
		}
		return add("quotes", values)
	}
	// 12000 + 2 x 1500 = 15000, plus 5% VAT = 15750; the default deposit is 50%.
	quote("Mainland LLC setup for Al Reem Trading", "sent", `[{"name": "Company setup service fee", "qty": 1, "price": 12000}, {"name": "PRO support", "qty": 2, "price": 1500}]`,
		map[string]string{"notes": "Government fees are paid by the client at cost."})
	quote("Draft offer", "draft", `[{"name": "Not ready", "price": 1}]`, nil)
	quote("Broken quote", "sent", `{"oops": true}`, nil)
	// 1500 + 75 VAT = 1575; 40% deposit = 630, balance 945, which may be paid at any time.
	quote("VAT registration", "sent", `[{"name": "VAT registration", "price": 1500}]`, map[string]string{"deposit_percent": "40", "balance_due": "1"})
	quote("Bookkeeping setup", "sent", `[{"name": "Bookkeeping setup", "price": 2100}]`, nil)
	quote("Old offer", "sent", `[{"name": "Licence renewal", "price": 1200}]`, map[string]string{"valid_until": "2020-01-01"})
	quote("Second opinion", "sent", `[{"name": "Review", "price": 500}]`, nil)
	// The admin panel refuses lines that are not JSON at all.
	if _, err := p.AdminSave(ctx, app, "quotes", 0, map[string]string{"enquiry_id": "1", "title": "x", "status": "sent", "lines": "[{name"}, "owner@bayan.ae"); err == nil {
		t.Fatal("invalid JSON lines were saved")
	}
	// on_admin_change emailed the client about each sent quote right away; the expired, broken and draft ones are not announced.
	const unsent = "email not sent: no mail service configured: "
	has("emails after the quotes were written", liveMails(), unsent+"Your quote #1 from Bayan Business Setup is ready", unsent+"Your quote #4 from Bayan Business Setup is ready",
		unsent+"Your quote #5 from Bayan Business Setup is ready", unsent+"Your quote #7 from Bayan Business Setup is ready")
	hasNot("emails after the quotes were written", liveMails(), "quote #2", "quote #3", "quote #6")
	if logs, _ := p.Logs(app, logFilterAll()); !anyLog(logs, "on_admin_change admin_change after insert quotes #5 by owner@bayan.ae -> ok") || !anyLog(logs, "quote 3 not announced") {
		t.Error("the admin-change runs are not in the log")
	}
	if q := row("quotes", 1); q["notified"] != "1" || q["valid_until"] == "" {
		t.Fatalf("announced quote: %v", q)
	}

	// The client sees sent quotes only; a quote whose lines cannot be read shows a kind message.
	portal := page(fatima, "/portal")
	has("portal", portal, "Mainland LLC setup for Al Reem Trading", "AED 15750.00", "Waiting for your answer", "Old offer", "Expired")
	hasNot("portal", portal, "Draft offer")
	q1 := page(fatima, "/quotes/1")
	has("quote", q1, "PRO support × 2", "AED 3000.00", "AED 15000.00", "AED 750.00", "AED 15750.00", "AED 7875.00 (50%)", "Government fees are paid", "Accept quote")
	hasNot("quote before acceptance", q1, "Pay by card")
	if code, _ := get(fatima, "/quotes/2"); code != 404 {
		t.Fatalf("draft quote: %d", code)
	}
	has("broken quote", page(fatima, "/quotes/3"), "This quote is being updated")
	if logs, _ := p.Logs(app, logFilterAll()); !anyLog(logs, "quote 3 has lines that cannot be read") {
		t.Error("no warning about the broken quote in the log")
	}
	if _, loc := post(fatima, "/quotes/3/accept", url.Values{"name": {"Fatima"}, "agree": {"1"}}); loc != "/quotes/3?error=error_unavailable" {
		t.Fatalf("accepting a broken quote: %s", loc)
	}
	if _, loc := post(fatima, "/quotes/6/accept", url.Values{"name": {"Fatima"}, "agree": {"1"}}); loc != "/quotes/6?error=error_expired" {
		t.Fatalf("accepting an expired quote: %s", loc)
	}
	has("expired quote", page(fatima, "/quotes/6?error=error_expired"), "This quote has expired", "<span class=\"badge\">Expired</span>")

	// Another client sees nothing of it and can do nothing with it.
	if code, _ := get(omar, "/quotes/1"); code != 404 {
		t.Fatalf("someone else's quote: %d", code)
	}
	for _, path := range []string{"/quotes/1/accept", "/quotes/1/decline", "/quotes/1/pay", "/quotes/1/message"} {
		if code, _ := post(omar, path, url.Values{"name": {"Omar"}, "agree": {"1"}, "method": {"transfer"}, "text": {"hello"}}); code != 404 {
			t.Fatalf("someone else's %s: %d", path, code)
		}
	}
	has("someone else's portal", page(omar, "/portal"), "You have no enquiries yet")

	// Accepting needs the name and the tick; then the totals are stored.
	if _, loc := post(fatima, "/quotes/1/accept", url.Values{"name": {"Fatima Al Marri"}}); loc != "/quotes/1?error=error_accept" {
		t.Fatalf("accept without the tick: %s", loc)
	}
	if _, loc := post(fatima, "/quotes/1/pay", url.Values{"method": {"transfer"}}); loc != "/quotes/1?error=error_unavailable" {
		t.Fatalf("paying before accepting: %s", loc)
	}
	if _, loc := post(fatima, "/quotes/1/accept", url.Values{"name": {"Fatima Al Marri"}, "agree": {"1"}}); loc != "/quotes/1?done=done_accepted" {
		t.Fatalf("accept: %s", loc)
	}
	if q := row("quotes", 1); q["status"] != "accepted" || q["subtotal"] != "15000" || q["vat"] != "750" || q["total"] != "15750" || q["deposit_amount"] != "7875" ||
		q["accepted_name"] != "Fatima Al Marri" || q["accepted_by"] == "" || q["accepted_at"] == "" {
		t.Fatalf("accepted quote: %v", q)
	}
	if e := row("enquiries", 1); e["status"] != "won" || e["user_id"] == "" {
		t.Fatalf("enquiry after acceptance: %v", e)
	}
	// What the client accepted stays, whatever the owner changes afterwards.
	edit("quotes", 1, map[string]string{"lines": `[{"name": "Changed later", "price": 1}]`})
	q1 = page(fatima, "/quotes/1?done=done_accepted")
	has("accepted quote", q1, "Your acceptance is recorded", "Accepted, payment due", "Accepted by Fatima Al Marri", "PRO support × 2", "AED 15750.00", "Due now: Deposit, AED 7875.00")
	hasNot("accepted quote", q1, "Changed later", "Accept quote")

	// Card payments are not connected on this live app: the client is told and can choose a transfer.
	if _, loc := post(fatima, "/quotes/1/pay", url.Values{"method": {"card"}}); loc != "/quotes/1?error=error_payment" {
		t.Fatalf("card without Stripe: %s", loc)
	}
	if _, loc := post(fatima, "/quotes/1/pay", url.Values{"method": {"transfer"}}); loc != "/quotes/1?done=done_transfer" {
		t.Fatalf("transfer: %s", loc)
	}
	post(fatima, "/quotes/1/pay", url.Values{"method": {"transfer"}}) // asking twice keeps one pending row
	has("transfer details", page(fatima, "/quotes/1?done=done_transfer"), "IBAN: AE00 0000", "Q1-deposit", "Waiting for the transfer", "AED 7875.00")
	rows, total, err := p.AdminList(ctx, app, "quote_payments", "transfer", 1)
	if err != nil || total != 1 || fmt.Sprint(rows[0]["status"]) != "pending" || fmt.Sprint(rows[0]["amount"]) != "7875" {
		t.Fatalf("ledger after the transfer was announced: %v %v", rows, err)
	}
	deposit := rows[0]["id"].(int64)
	// The owner marks the transfer paid; the client's page follows the ledger.
	edit("quote_payments", deposit, map[string]string{"status": "paid"})
	q1 = page(fatima, "/quotes/1")
	has("after the deposit", q1, "<span class=\"badge\">Deposit paid</span>", "We will ask for the balance", fmt.Sprintf(`href="/quotes/1/invoice/%d"`, deposit))
	has("receipt email", liveMails(), unsent+"Payment received: AED 7875.00 for quote #1")
	hasNot("after the deposit", q1, "Pay by card", "I will pay by bank transfer")
	if q := row("quotes", 1); q["status"] != "deposit_paid" || q["paid_amount"] != "7875" {
		t.Fatalf("stored quote after the deposit: %v", q)
	}
	if _, loc := post(fatima, "/quotes/1/pay", url.Values{"method": {"transfer"}}); loc != "/quotes/1?error=error_not_due" {
		t.Fatalf("balance before it is asked for: %s", loc)
	}
	edit("quotes", 1, map[string]string{"balance_due": "1"})
	has("balance asked for", page(fatima, "/quotes/1"), "Due now: Balance, AED 7875.00")
	has("balance email", liveMails(), unsent+"Balance due: AED 7875.00 for quote #1")
	if _, loc := post(fatima, "/quotes/1/pay", url.Values{"method": {"transfer"}}); loc != "/quotes/1?done=done_transfer" {
		t.Fatalf("balance by transfer: %s", loc)
	}
	rows, _, _ = p.AdminList(ctx, app, "quote_payments", "Q1-balance", 1)
	if len(rows) != 1 {
		t.Fatalf("balance row: %v", rows)
	}
	edit("quote_payments", rows[0]["id"].(int64), map[string]string{"status": "paid"})
	has("fully paid", page(fatima, "/quotes/1"), "<span class=\"badge\">Paid in full</span>", "AED 0.00")
	if q := row("quotes", 1); q["status"] != "paid" || q["paid_amount"] != "15750" {
		t.Fatalf("stored quote after the balance: %v", q)
	}
	// The receipt of the deposit: 7875 = 7500 + 375 VAT.
	invoice := fmt.Sprintf("/quotes/1/invoice/%d", deposit)
	has("receipt", page(fatima, invoice), "<h1>Receipt</h1>", fmt.Sprintf("INV-1-%d", deposit), "Bayan Corporate Services LLC", "Fatima Al Marri, Al Reem Trading",
		">7500.00<", ">375.00<", "AED 7875.00", "Deposit: Mainland LLC setup for Al Reem Trading", "Bank transfer")
	if code, _ := get(omar, invoice); code != 404 {
		t.Fatalf("someone else's receipt: %d", code)
	}
	if code, _ := get(fatima, "/quotes/4/invoice/"+fmt.Sprint(deposit)); code != 404 {
		t.Fatalf("receipt under another quote: %d", code)
	}

	// Progress updates and a reply from the firm.
	add("updates", map[string]string{"quote_id": "1", "title": "Trade licence issued", "text": "Your licence is ready.", "link": "https://drive.example/licence", "visible": "1"})
	add("updates", map[string]string{"quote_id": "1", "title": "Internal note", "visible": "0"})
	add("updates", map[string]string{"quote_id": "1", "title": "Bad link", "link": "javascript:alert(1)", "visible": "1"})
	if _, loc := post(fatima, "/quotes/1/message", url.Values{"text": {"When can I collect the licence?"}}); loc != "/quotes/1?done=done_message" {
		t.Fatalf("message: %s", loc)
	}
	add("messages", map[string]string{"quote_id": "1", "sender": "firm", "text": "Tomorrow from 9 am."})
	q1 = page(fatima, "/quotes/1")
	has("updates and messages", q1, "Trade licence issued", `href="https://drive.example/licence"`, "When can I collect the licence?", "Tomorrow from 9 am.")
	hasNot("updates", q1, "Internal note", "javascript:")
	has("update emails", liveMails(), unsent+"Update from Bayan Business Setup: Trade licence issued")
	hasNot("update emails", liveMails(), "Internal note")
	// That was the tenth email to the client today: the firm's reply waits for the hourly job (below, on preview).
	if logs, _ := p.Logs(app, logFilterAll()); !anyLog(logs, "email to client not sent: GoError: too many emails to this address today") {
		t.Error("the daily limit per address was not reached")
	}
	hasNot("reply email", liveMails(), "New message from Bayan Business Setup")

	if _, loc := post(fatima, "/quotes/7/decline", url.Values{"reason": {"Too expensive"}}); loc != "/quotes/7?done=done_declined" {
		t.Fatalf("decline: %s", loc)
	}
	if q := row("quotes", 7); q["status"] != "declined" || q["decline_reason"] != "Too expensive" {
		t.Fatalf("declined quote: %v", q)
	}

	// ---- Preview, a copy of live data: card payments (simulated) and jobs.
	apply()
	const client = "fatima@mail.ae"
	if r := as(client, "POST", "/quotes/4/accept", map[string]string{"name": "Fatima Al Marri", "agree": "1"}); r.Location != "/quotes/4?done=done_accepted" {
		t.Fatalf("accept on preview: %+v", r)
	}
	pay := func(path, wantAmount, decision string) platform.TestResponse {
		t.Helper()
		r := as(client, "POST", path, map[string]string{"method": "card"})
		if r.Status != 303 || !strings.HasPrefix(r.Location, "/_rx/pay/") {
			t.Fatalf("card checkout: %+v\n%s", r, logText(r))
		}
		if !strings.Contains(req(platform.TestRequestInput{Path: r.Location}).Body, wantAmount) {
			t.Fatalf("charged amount is not %s", wantAmount)
		}
		return req(platform.TestRequestInput{Method: "POST", Path: r.Location, Form: map[string]string{"decision": decision}})
	}
	// A failed card payment leaves the deposit unpaid and can be tried again.
	pay("/quotes/4/pay", "630.00 AED", "fail")
	q4 := as(client, "GET", "/quotes/4", nil).Body
	has("after a failed payment", q4, "The card payment did not go through", "Pay by card", "Accepted, payment due", "Due now: Deposit, AED 630.00")
	// The address alone does not make a payment.
	hasNot("forged success address", as(client, "GET", "/quotes/4?done=done_paid", nil).Body, "We received your payment", "Deposit paid")
	if paid := pay("/quotes/4/pay", "630.00 AED", "pay"); paid.Location != "/quotes/4?done=done_paid" {
		t.Fatalf("after paying: %+v", paid)
	}
	q4 = as(client, "GET", "/quotes/4?done=done_paid", nil).Body
	has("after the deposit by card", q4, "We received your payment", "<span class=\"badge\">Deposit paid</span>", "Due now: Balance, AED 945.00")
	hasNot("after the deposit by card", q4, "did not go through")
	pay("/quotes/4/pay", "945.00 AED", "pay")
	q4 = as(client, "GET", "/quotes/4", nil).Body
	has("after the balance by card", q4, "<span class=\"badge\">Paid in full</span>", "AED 1575.00")
	hasNot("after the balance by card", q4, "Pay by card")
	if r := as(client, "POST", "/quotes/4/pay", map[string]string{"method": "card"}); r.Location != "/quotes/4?done=done_paid" {
		t.Fatalf("paying a paid quote: %+v", r)
	}
	// Card rows of quote 4 follow the three ledger rows made on live: the balance of 945 = 900 + 45 VAT.
	has("receipt of the balance", as(client, "GET", "/quotes/4/invoice/5", nil).Body, ">900.00<", ">45.00<", "AED 945.00", "Balance: VAT registration", "Card")
	has("emails to the client", mails(), `not sent from preview: to fatima@mail.ae, "You accepted quote #4 from Bayan Business Setup"`,
		`not sent from preview: to fatima@mail.ae, "Payment received: AED 630.00 for quote #4"`, "/quotes/4/invoice/4")

	// Privacy holds on preview too.
	if r := as("omar@mail.ae", "GET", "/quotes/4", nil); r.Status != 404 {
		t.Fatalf("someone else's quote on preview: %d", r.Status)
	}
	if r := as("omar@mail.ae", "GET", "/quotes/4/invoice/5", nil); r.Status != 404 {
		t.Fatalf("someone else's receipt on preview: %d", r.Status)
	}

	// A new enquiry tells the owner; Arabic pages are right to left.
	r := as("omar@mail.ae", "POST", "/enquiry", map[string]string{"name": "Omar", "phone": "+971509999999", "message": "Wedding photos in March", "lang": "ar"})
	if r.Location != "/portal?done=done_enquiry&lang=ar" {
		t.Fatalf("enquiry on preview: %+v\n%s", r, logText(r))
	}
	has("notice to the owner", mails(), `to the owner, "New enquiry #2: custom request from Omar, +971509999999"`)
	has("arabic portal", as("omar@mail.ae", "GET", r.Location, nil).Body, `dir="rtl"`, "طلب خاص", "Wedding photos in March")
	has("arabic quote", as(client, "GET", "/quotes/5?lang=ar", nil).Body, `dir="rtl"`, "عرض سعر #5", "بانتظار ردك", "AED 2205.00")

	// Messages: not empty, not too long, and at most 20 a day per quote.
	if r := as(client, "POST", "/quotes/5/message", map[string]string{"text": "  "}); r.Location != "/quotes/5?error=error_message" {
		t.Fatalf("empty message: %+v", r)
	}
	if r := as(client, "POST", "/quotes/5/message", map[string]string{"text": strings.Repeat("x", 1001)}); r.Location != "/quotes/5?error=error_message" {
		t.Fatalf("long message: %+v", r)
	}
	for i := 1; i <= 20; i++ {
		r := as(client, "POST", "/quotes/5/message", map[string]string{"text": fmt.Sprintf("Question %d", i)})
		if r.Location != "/quotes/5?done=done_message" {
			t.Fatalf("message %d: %+v", i, r)
		}
		if i == 1 {
			has("notice to the owner", mails(), `to the owner, "Message on quote #5 from Fatima Al Marri: Question 1"`)
		}
	}
	if r := as(client, "POST", "/quotes/5/message", map[string]string{"text": "One more"}); r.Location != "/quotes/5?error=error_many_messages" {
		t.Fatalf("message 21: %+v", r)
	}
	q5 := as(client, "GET", "/quotes/5?error=error_many_messages", nil).Body
	has("messages", q5, "Question 20", "You sent many messages today")
	hasNot("messages", q5, "One more")

	// The hourly job sends what hit the daily limit, once.
	job := func(name string) platform.TestResponse {
		t.Helper()
		r, err := p.TestRequest(ctx, app, platform.TestRequestInput{Job: name})
		if err != nil || r.Status != 200 {
			t.Fatalf("job %s: %v %+v\n%s", name, err, r, logText(r))
		}
		return r
	}
	// The firm's reply on quote 1 is all that is left.
	n := job("notify_clients")
	if n.Body != "1 emails sent" {
		t.Fatalf("notify_clients: %s\n%s", n.Body, logText(n))
	}
	has("emails of notify_clients", mails(), `not sent from preview: to fatima@mail.ae, "New message from Bayan Business Setup about quote #1"`, "Tomorrow from 9 am.")
	hasNot("emails of notify_clients", mails(), "Internal note", "Old offer", "Draft offer", "quote #7", "quote #3", "Your quote #5", "Trade licence issued")
	if logs, _ := p.Logs(app, logFilterAll()); !anyLog(logs, "quote 3 not announced") {
		t.Error("no warning about the quote that could not be announced")
	}
	if again := job("notify_clients"); again.Body != "0 emails sent" {
		t.Fatalf("notify_clients a second time: %s", again.Body)
	}

	// The morning job expires quotes past their date and reports to the owner: quotes 3 and 5 still wait for the client.
	d := job("daily_summary")
	if d.Body != "1 new enquiries, 2 quotes waiting, 0 transfers pending, 1 expired" {
		t.Fatalf("daily_summary: %s", d.Body)
	}
	has("daily summary", mails(), `to the owner, "Today: 1 new enquiry, 2 quotes waiting for the client, AED 0.00 in transfers to confirm"`, "#2 Omar, +971509999999",
		"Quotes expired today: 1")
	if again := job("daily_summary"); again.Body != "1 new enquiries, 2 quotes waiting, 0 transfers pending, 0 expired" {
		t.Fatalf("daily_summary a second time: %s", again.Body)
	}
	has("expired quote", as(client, "GET", "/quotes/6", nil).Body, "<span class=\"badge\">Expired</span>")

	// The owner's changes on preview, made with test_request: a new quote is emailed when it is set to sent.
	admin := func(in platform.TestAdminInput) platform.TestResponse {
		t.Helper()
		r, err := p.TestRequest(ctx, app, platform.TestRequestInput{Admin: &in})
		if err != nil || r.Status != 200 {
			t.Fatalf("admin %+v: %v %+v\n%s", in, err, r, logText(r))
		}
		return r
	}
	a := admin(platform.TestAdminInput{Action: "insert", Table: "quotes", By: "owner@bayan.ae", Values: map[string]any{"enquiry_id": 1, "title": "Annual audit",
		"status": "draft", "lines": `[{"name": "Audit of the 2025 accounts", "price": 4000}]`}})
	if a.Body != "0 emails sent" || a.Admin.Row["notified"] != int64(0) {
		t.Fatalf("draft quote: %s %v", a.Body, a.Admin.Row)
	}
	a = admin(platform.TestAdminInput{Action: "update", Table: "quotes", ID: a.Admin.ID, Values: map[string]any{"status": "sent"}})
	if a.Body != "1 emails sent" || a.Admin.Row["notified"] != int64(1) || a.Admin.Row["title"] != "Annual audit" || !anyLog(a.Logs, "on_admin_change admin_change after update quotes") {
		t.Fatalf("sending the quote: %s %v\n%s", a.Body, a.Admin.Row, logText(a))
	}
	has("quote email on preview", mails(), `not sent from preview: to fatima@mail.ae, "Your quote #8 from Bayan Business Setup is ready"`, "Total: AED 4200.00", "/quotes/8\n")
	has("the new quote", as(client, "GET", "/quotes/8", nil).Body, "Annual audit", "AED 4200.00", "Accept quote")

	// ---- Prices that include VAT, and a TRN: 2100 holds 100 VAT, and receipts become tax invoices.
	edit("business", 1, map[string]string{"prices_include_vat": "1", "trn": "100123456700003"})
	apply()
	q5 = as(client, "GET", "/quotes/5", nil).Body
	has("quote with VAT included", q5, "AED 2000.00", "AED 100.00", "AED 2100.00", "The prices above include VAT", "AED 1050.00 (50%)")
	if r := as(client, "POST", "/quotes/5/accept", map[string]string{"name": "Fatima Al Marri", "agree": "1"}); r.Location != "/quotes/5?done=done_accepted" {
		t.Fatalf("accept with VAT included: %+v", r)
	}
	pay("/quotes/5/pay", "1050.00 AED", "pay")
	// The accepted quote 1 keeps its totals; its receipt is now a tax invoice.
	has("tax invoice", as(client, "GET", invoice, nil).Body, "<h1>Tax invoice</h1>", "TRN: 100123456700003", ">7500.00<", ">375.00<")
	has("package with VAT included", req(platform.TestRequestInput{Path: "/packages/5"}).Body, "including VAT")
}

func TestQuotePortalUAETools(t *testing.T) {
	p := newPlatform(t)
	ctx := context.Background()
	ex, _ := examples.Get("quote-portal-uae")
	def, problems := appdef.Parse(ex.JSON)
	if len(problems) > 0 {
		t.Fatal(problems)
	}
	p.CreateApp("usr_test", "bayan")
	if res, err := p.ApplyChange(ctx, "bayan", def); err != nil || !res.OK {
		t.Fatal(err, res)
	}
	call := func(name string, args map[string]any) string {
		t.Helper()
		r, err := p.TestRequest(ctx, "bayan", platform.TestRequestInput{Method: "POST", Path: "/mcp",
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
	if out := call("list_packages", map[string]any{"category": "tax and accounting"}); !strings.Contains(out, `"name":"VAT registration"`) ||
		!strings.Contains(out, `"price_from":1500`) || !strings.Contains(out, `"url":"https://bayan--preview.renoxium.test/packages/5"`) ||
		strings.Contains(out, "Golden Visa") || !strings.Contains(out, `"prices_include_vat":false`) {
		t.Errorf("list_packages: %s", out)
	}
	if out := call("list_packages", map[string]any{"lang": "ar"}); !strings.Contains(out, "طلب الإقامة الذهبية") || !strings.Contains(out, "/packages/3?lang=ar") {
		t.Errorf("list_packages in Arabic: %s", out)
	}
	if out := call("business_info", nil); !strings.Contains(out, `"payment_methods":["card","bank transfer"]`) || !strings.Contains(out, `"deposit_percent":50`) ||
		!strings.Contains(out, `"legal_name":"Bayan Corporate Services LLC"`) {
		t.Errorf("business_info: %s", out)
	}
	if out := call("how_to_get_a_quote", nil); !strings.Contains(out, `"enquiry_url":"https://bayan--preview.renoxium.test/enquiry"`) ||
		!strings.Contains(out, "Accept and pay the deposit") || !strings.Contains(out, `"quote_valid_days":14`) {
		t.Errorf("how_to_get_a_quote: %s", out)
	}
	// With nothing to do, the jobs say so and send nothing.
	for name, want := range map[string]string{"notify_clients": "0 emails sent", "daily_summary": "nothing to report"} {
		if r, err := p.TestRequest(ctx, "bayan", platform.TestRequestInput{Job: name}); err != nil || r.Status != 200 || r.Body != want {
			t.Errorf("%s without data: %v %+v", name, err, r)
		}
	}
}
