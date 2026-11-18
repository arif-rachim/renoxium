package platform_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/arif-rachim/renoxium/examples"
	"github.com/arif-rachim/renoxium/internal/appdef"
	"github.com/arif-rachim/renoxium/internal/platform"
)

// The UAE home-service example works end to end on preview: browsing in two
// languages, arrival windows with limited crews, prices worked out on the
// server, cash and card (simulated) bookings, the crew's private page, live
// status, cancelling, repeat visits from the daily job and the MCP tools.
func TestHomeServiceUAEExample(t *testing.T) {
	p := newPlatform(t)
	ctx := context.Background()
	ex, ok := examples.Get("home-service-uae")
	if !ok {
		t.Fatal("example not registered")
	}
	def, problems := appdef.Parse(ex.JSON)
	if len(problems) > 0 {
		t.Fatal(problems)
	}
	p.CreateApp("usr_test", "bayti")
	res, err := p.ApplyChange(ctx, "bayti", def)
	if err != nil || !res.OK {
		t.Fatal(err, res)
	}
	req := func(in platform.TestRequestInput) platform.TestResponse {
		t.Helper()
		r, err := p.TestRequest(ctx, "bayti", in)
		if err != nil {
			t.Fatalf("%+v: %v", in, err)
		}
		if r.Status >= 500 {
			t.Fatalf("%s %s: %d\n%s\n%s", in.Method, in.Path, r.Status, r.Body, logText(r))
		}
		return r
	}
	logged := func(want string) bool {
		t.Helper()
		logs, _ := p.Logs("bayti", logFilterAll())
		return anyLog(logs, want)
	}

	// The example keeps UAE dates (UTC+4). The seed is closed on Sundays, so
	// the visit is three days ahead, or four when that is a Sunday; a week
	// later is still inside the 14 bookable days.
	dubai := time.Now().UTC().Add(4 * time.Hour)
	visit := dubai.AddDate(0, 0, 3)
	if visit.Weekday() == time.Sunday {
		visit = visit.AddDate(0, 0, 1)
	}
	sunday := dubai.AddDate(0, 0, 1)
	for sunday.Weekday() != time.Sunday {
		sunday = sunday.AddDate(0, 0, 1)
	}
	day, nextWeek := visit.Format("2006-01-02"), visit.AddDate(0, 0, 7).Format("2006-01-02")
	type slot struct {
		Start, End string
		Left       int
	}
	slots := func(date string) (out struct {
		Closed string
		Slots  []slot
	}) {
		t.Helper()
		r := req(platform.TestRequestInput{Path: "/api/slots?date=" + date})
		if err := json.Unmarshal([]byte(r.Body), &out); err != nil {
			t.Fatalf("slots: %v: %s", err, r.Body)
		}
		return out
	}
	left := func(date, start string) int {
		t.Helper()
		for _, s := range slots(date).Slots {
			if s.Start == start {
				return s.Left
			}
		}
		t.Fatalf("no window %s on %s", start, date)
		return 0
	}

	home := req(platform.TestRequestInput{Path: "/"})
	for _, want := range []string{"Bayti Home Services", "Home cleaning", "AED 40.00", "Pest control", "Arabian Ranches · + AED 25.00", `dir="ltr"`} {
		if !strings.Contains(home.Body, want) {
			t.Fatalf("home lacks %q: %s", want, home.Body)
		}
	}
	ar := req(platform.TestRequestInput{Path: "/?lang=ar"})
	if !strings.Contains(ar.Body, `dir="rtl"`) || !strings.Contains(ar.Body, "تنظيف المنزل") || !strings.Contains(ar.Body, "بيتي للخدمات المنزلية") ||
		!strings.Contains(ar.Body, `href="/s/1?lang=ar"`) {
		t.Fatal("arabic home")
	}
	if r := req(platform.TestRequestInput{Path: "/s/1"}); r.Status != 200 || !strings.Contains(r.Body, "Materials and equipment (+ AED 10.00 / hour)") ||
		!strings.Contains(r.Body, `<option value="`+day+`">`) || strings.Contains(r.Body, `<option value="`+sunday.Format("2006-01-02")+`">`) {
		t.Fatalf("service page: %d %s", r.Status, r.Body)
	}
	if r := req(platform.TestRequestInput{Path: "/s/999"}); r.Status != 404 {
		t.Fatalf("missing service: %d", r.Status)
	}

	// Six two-hour windows from 08:00 to 18:00 with two crews each; none on the closed day or in the past.
	if s := slots(day); len(s.Slots) != 6 || s.Slots[0] != (slot{"08:00", "10:00", 2}) || s.Slots[5].Start != "18:00" || s.Closed != "" {
		t.Fatalf("slots: %+v", s)
	}
	if s := slots(sunday.Format("2006-01-02")); len(s.Slots) != 0 || s.Closed == "" {
		t.Fatalf("closed day: %+v", s)
	}
	if s := slots(dubai.AddDate(0, 0, -1).Format("2006-01-02")); len(s.Slots) != 0 {
		t.Fatalf("yesterday: %+v", s)
	}

	form := func(service, qty, zone, start, payment string) map[string]string {
		return map[string]string{"service": service, "qty": qty, "zone": zone, "date": day, "slot": start, "repeat": "once", "payment": payment,
			"name": "Fatima", "phone": "+971501234567", "address": "Villa 12, Street 4", "flat": "Villa 12", "notes": "Gate code 4411",
			"total": "1", "price": "1"} // prices from the browser must be ignored
	}
	// Signed out: the checkout page asks to sign in and comes back; a POST is refused.
	query := "/checkout?service=1&qty=3&materials=1&zone=5&date=" + day + "&slot=10:00&repeat=weekly"
	if r := req(platform.TestRequestInput{Path: query}); r.Status != 303 || !strings.HasPrefix(r.Location, "/_rx/auth/login?next=") {
		t.Fatalf("signed out checkout: %+v", r)
	}
	if r := req(platform.TestRequestInput{Method: "POST", Path: "/book", Form: form("1", "3", "5", "10:00", "cash")}); r.Status != 401 {
		t.Fatalf("signed out booking: %d", r.Status)
	}
	req(platform.TestRequestInput{Method: "POST", Path: "/_rx/auth/signup", Form: map[string]string{"name": "Fatima", "email": "fatima@mail.ae", "password": "fatima password"}})
	as := func(in platform.TestRequestInput) platform.TestResponse {
		t.Helper()
		in.UserEmail = "fatima@mail.ae"
		return req(in)
	}
	book := func(f map[string]string) platform.TestResponse {
		t.Helper()
		return as(platform.TestRequestInput{Method: "POST", Path: "/book", Form: f})
	}

	// Server-side price: 3 hours x 40 + materials 3 x 10 + Arabian Ranches 25 = 175, VAT included = 175 * 5/105.
	if r := as(platform.TestRequestInput{Path: query}); r.Status != 200 || !strings.Contains(r.Body, "AED 175.00") || !strings.Contains(r.Body, "AED 8.33") ||
		!strings.Contains(r.Body, `value="Fatima"`) || !strings.Contains(r.Body, "Every week") || !strings.Contains(r.Body, "Pay on completion") {
		t.Fatalf("checkout page: %d %s", r.Status, r.Body)
	}

	// Cash on completion is confirmed at once; the customer and the owner are told (logged on preview).
	f := form("1", "3", "5", "10:00", "cash")
	f["materials"], f["repeat"] = "1", "weekly"
	r := book(f)
	if r.Location != "/bookings/1?placed=1" {
		t.Fatalf("cash booking: %+v\n%s", r, logText(r))
	}
	page := as(platform.TestRequestInput{Path: "/bookings/1?placed=1"})
	for _, want := range []string{"Thank you", `id="status">Confirmed<`, "Home cleaning × 3", "AED 120.00", "Materials and equipment × 3", "AED 30.00", "AED 25.00",
		"AED 175.00", "AED 8.33", "Every week", "Pay on completion (cash)", "/_rx/live/booking-1?token=", "Cancel this and all later visits"} {
		if !strings.Contains(page.Body, want) {
			t.Errorf("booking page lacks %q", want)
		}
	}
	for _, want := range []string{`not sent from preview: to fatima@mail.ae, "Booking #1 confirmed: Home cleaning on ` + day + `"`,
		"https://bayti--preview.renoxium.test/bookings/1",
		`to the owner, "New booking #1 (cash): Home cleaning, ` + day + ` 10:00, Arabian Ranches, AED 175.00"`} {
		if !logged(want) {
			t.Errorf("missing in log: %s", want)
		}
	}
	if n := left(day, "10:00"); n != 1 {
		t.Fatalf("crews left after one booking = %d", n)
	}

	// Card: 2 AC units x 120 in Dubai Marina, no surcharge. The unpaid booking already holds its window.
	r = book(form("4", "2", "1", "10:00", "card"))
	if r.Status != 303 || !strings.HasPrefix(r.Location, "/_rx/pay/") {
		t.Fatalf("card booking: %+v\n%s", r, logText(r))
	}
	if !strings.Contains(req(platform.TestRequestInput{Path: r.Location}).Body, "240.00 AED") {
		t.Fatal("charged amount")
	}
	if o := as(platform.TestRequestInput{Path: "/bookings/2"}); !strings.Contains(o.Body, "Waiting for payment") || !strings.Contains(o.Body, "Pay by card now") {
		t.Fatal("pending booking page")
	}
	// The window is full: a third booking for it is refused, also in Arabic, and the form says why.
	if n := left(day, "10:00"); n != 0 {
		t.Fatalf("crews left in a full window = %d", n)
	}
	if x := book(form("6", "1", "1", "10:00", "cash")); x.Location != "/s/6?error=error_slot" {
		t.Fatalf("booking a full window: %+v", x)
	}
	full := form("6", "1", "1", "10:00", "cash")
	full["lang"] = "ar"
	if x := book(full); x.Location != "/s/6?error=error_slot&lang=ar" {
		t.Fatalf("booking a full window in Arabic: %+v", x)
	}
	if x := req(platform.TestRequestInput{Path: "/s/6?error=error_slot"}); !strings.Contains(x.Body, "no longer free") {
		t.Fatal("slot error message")
	}
	paid := req(platform.TestRequestInput{Method: "POST", Path: r.Location, Form: map[string]string{"decision": "pay"}})
	if paid.Location != "/bookings/2?placed=1" {
		t.Fatalf("after paying: %+v", paid)
	}
	if o := as(platform.TestRequestInput{Path: "/bookings/2"}); !strings.Contains(o.Body, `id="status">Confirmed<`) || !strings.Contains(o.Body, "Paid by card") ||
		strings.Contains(o.Body, "Pay by card now") {
		t.Fatalf("paid booking page: %s", o.Body)
	}
	if !logged(`to the owner, "New booking #2 (card, paid): AC service, ` + day + ` 10:00, Dubai Marina, AED 240.00"`) {
		t.Error("owner not told about the paid booking")
	}

	// A failed card payment frees the window again.
	r = book(form("6", "1", "2", "12:00", "card"))
	if n := left(day, "12:00"); n != 1 {
		t.Fatalf("crews left before the failure = %d", n)
	}
	req(platform.TestRequestInput{Method: "POST", Path: r.Location, Form: map[string]string{"decision": "fail"}})
	if n := left(day, "12:00"); n != 2 {
		t.Fatalf("crews left after the failure = %d", n)
	}
	if o := as(platform.TestRequestInput{Path: "/bookings/3"}); !strings.Contains(o.Body, "Payment failed") || !strings.Contains(o.Body, "Book again") {
		t.Fatal("failed booking page")
	}

	// Bad choices go back to the service page with a message; missing fields go back to the checkout page.
	for want, change := range map[string][2]string{"error_quantity": {"qty", "1"}, "error_zone": {"zone", "99"}, "error_slot": {"slot", "09:00"}} {
		f := form("1", "3", "1", "14:00", "cash")
		f[change[0]] = change[1]
		if x := book(f); x.Location != "/s/1?error="+want {
			t.Errorf("%s=%s: %+v", change[0], change[1], x)
		}
	}
	f = form("1", "3", "1", "14:00", "cash")
	f["address"] = ""
	if x := book(f); !strings.HasPrefix(x.Location, "/checkout?service=1&qty=3&") || !strings.HasSuffix(x.Location, "&error=error_fields") {
		t.Fatalf("missing address: %+v", x)
	}

	// The crew's private link is in the owner's notice. A wrong key shows nothing and changes nothing.
	logs, _ := p.Logs("bayti", logFilterAll())
	var key string
	for _, e := range logs {
		if m := regexp.MustCompile(`/crew/1\?key=([A-Za-z0-9_-]{20,})`).FindStringSubmatch(e.Message); m != nil {
			key = m[1]
		}
	}
	if key == "" {
		t.Fatal("no crew link in the owner's notice")
	}
	// The key is crypto.randomId(): 16 random bytes, 22 URL-safe characters.
	if !regexp.MustCompile(`^[A-Za-z0-9_-]{22}$`).MatchString(key) {
		t.Fatalf("crew key %q is not a crypto.randomId()", key)
	}
	if x := req(platform.TestRequestInput{Path: "/crew/1?key=wrong"}); x.Status != 404 {
		t.Fatalf("crew page with a wrong key: %d", x.Status)
	}
	if x := req(platform.TestRequestInput{Path: "/crew/1"}); x.Status != 404 {
		t.Fatalf("crew page without a key: %d", x.Status)
	}
	crew := func(action string, f map[string]string) platform.TestResponse {
		t.Helper()
		f["key"] = key
		return req(platform.TestRequestInput{Method: "POST", Path: "/crew/1/" + action, Form: f})
	}
	if x := req(platform.TestRequestInput{Method: "POST", Path: "/crew/1/status", Form: map[string]string{"key": "wrong", "status": "completed"}}); x.Status != 403 {
		t.Fatalf("status with a wrong key: %d", x.Status)
	}
	if x := req(platform.TestRequestInput{Path: "/crew/1?key=" + key}); x.Status != 200 || !strings.Contains(x.Body, "Cash to collect: AED 175.00") ||
		!strings.Contains(x.Body, "Gate code 4411") || !strings.Contains(x.Body, "Arrived / start") {
		t.Fatalf("crew page: %d %s", x.Status, x.Body)
	}
	// Positions count only while on the way; each status change reaches the customer's booking.
	pos := map[string]string{"lat": "25.05", "lng": "55.26"}
	if x := crew("position", pos); x.Status != 409 {
		t.Fatalf("position before leaving: %d", x.Status)
	}
	if x := crew("status", map[string]string{"status": "completed"}); x.Status != 409 {
		t.Fatalf("completing a job that has not started: %d", x.Status)
	}
	if x := crew("status", map[string]string{"status": "on_the_way"}); x.Status != 200 {
		t.Fatalf("on the way: %+v", x)
	}
	if x := crew("position", pos); x.Status != 200 {
		t.Fatalf("position: %+v", x)
	}
	if o := as(platform.TestRequestInput{Path: "/bookings/1"}); !strings.Contains(o.Body, `id="status">Crew on the way<`) || !strings.Contains(o.Body, "start=[ 25.05 , 55.26 ]") ||
		strings.Contains(o.Body, "Cancel this visit") {
		t.Fatalf("booking while the crew is on the way: %s", o.Body)
	}
	if x := crew("status", map[string]string{"status": "in_progress"}); x.Status != 200 {
		t.Fatalf("start: %+v", x)
	}
	if x := crew("status", map[string]string{"status": "completed"}); x.Status != 200 {
		t.Fatalf("completed: %+v", x)
	}
	if o := as(platform.TestRequestInput{Path: "/bookings/1"}); !strings.Contains(o.Body, `id="status">Completed<`) || !strings.Contains(o.Body, "Cash received") {
		t.Fatalf("completed booking: %s", o.Body)
	}
	if !logged(`to the owner, "Booking #1 completed, cash collected AED 175.00"`) {
		t.Error("owner not told about the completed job")
	}

	// Receipt: 3 x 40 incl. VAT = 3 x 38.10 net; totals 166.67 + 8.33. A tax invoice needs the TRN.
	inv := as(platform.TestRequestInput{Path: "/bookings/1/receipt"})
	for _, want := range []string{"<h1>Receipt</h1>", "Bayti Home Services LLC", ">38.10<", "Area surcharge (Arabian Ranches)", "AED 166.67", "AED 8.33", "AED 175.00", "Cash received"} {
		if !strings.Contains(inv.Body, want) {
			t.Errorf("receipt lacks %q", want)
		}
	}
	if x := as(platform.TestRequestInput{Path: "/bookings/3/receipt"}); x.Status != 404 {
		t.Fatalf("receipt of a failed booking: %d", x.Status)
	}

	// Bookings are private: another visitor sees neither the list entry nor the page, and cannot cancel.
	req(platform.TestRequestInput{Method: "POST", Path: "/_rx/auth/signup", Form: map[string]string{"name": "Omar", "email": "omar@mail.ae", "password": "omar password"}})
	other := func(in platform.TestRequestInput) platform.TestResponse {
		t.Helper()
		in.UserEmail = "omar@mail.ae"
		return req(in)
	}
	for _, path := range []string{"/bookings/1", "/bookings/1/receipt", "/bookings/2"} {
		if x := other(platform.TestRequestInput{Path: path}); x.Status != 404 {
			t.Fatalf("someone else's %s: %d", path, x.Status)
		}
	}
	if x := other(platform.TestRequestInput{Method: "POST", Path: "/bookings/2/cancel"}); x.Status != 404 {
		t.Fatalf("cancelling someone else's booking: %d", x.Status)
	}
	if x := other(platform.TestRequestInput{Path: "/bookings"}); strings.Contains(x.Body, "Booking #1<") || !strings.Contains(x.Body, "no bookings yet") {
		t.Fatal("someone else's booking listed")
	}
	if x := as(platform.TestRequestInput{Path: "/bookings"}); !strings.Contains(x.Body, "Booking #1<") || !strings.Contains(x.Body, "Booking #2<") {
		t.Fatal("own bookings list")
	}

	// The customer cancels the paid card booking before the cut-off: its window is free again, the card is refunded
	// (payments.refund) and the owner is told.
	if x := as(platform.TestRequestInput{Method: "POST", Path: "/bookings/2/cancel", Form: map[string]string{"later": "0"}}); x.Location != "/bookings/2" {
		t.Fatalf("cancel: %+v", x)
	}
	if o := as(platform.TestRequestInput{Path: "/bookings/2"}); !strings.Contains(o.Body, `id="status">Cancelled<`) || strings.Contains(o.Body, "Cancel this visit") ||
		!strings.Contains(o.Body, "Refunded to your card: AED 240.00") {
		t.Fatalf("cancelled booking page: %s", o.Body)
	}
	if n := left(day, "10:00"); n != 1 {
		t.Fatalf("crews left after cancelling = %d", n)
	}
	if !logged(`Booking #2 cancelled by the customer: AC service, ` + day + ` 10:00, refunded AED 240.00`) {
		t.Error("owner not told about the cancellation")
	}
	if !logged("a script refunded AED 240.00 of the payment for booking-2 (cancelled by the customer)") {
		t.Error("card payment not refunded")
	}
	if x := as(platform.TestRequestInput{Method: "POST", Path: "/bookings/1/cancel"}); x.Status != 409 {
		t.Fatalf("cancelling a completed booking: %d", x.Status)
	}

	// Repeat visits. Omar books 16:00 every week, but next week's 16:00 is already full: his repeat is skipped.
	// Fatima's weekly cleaning (booking 1) gets its next visit in the same window, to pay on completion or by card.
	omar := func(date, repeat string) string {
		t.Helper()
		f := form("6", "1", "1", "16:00", "cash")
		f["date"], f["repeat"], f["name"] = date, repeat, "Omar"
		x := other(platform.TestRequestInput{Method: "POST", Path: "/book", Form: f})
		if !strings.HasPrefix(x.Location, "/bookings/") {
			t.Fatalf("omar's booking: %+v", x)
		}
		return strings.TrimSuffix(strings.TrimPrefix(x.Location, "/bookings/"), "?placed=1")
	}
	weekly := omar(day, "weekly")
	omar(nextWeek, "once")
	omar(nextWeek, "once")
	job, err := p.TestRequest(ctx, "bayti", platform.TestRequestInput{Job: "daily"})
	if err != nil || job.Status != 200 || job.Body != "1 repeat visits booked, 1 skipped, 0 reminders, 0 visits tomorrow" {
		t.Fatalf("daily job: %v %+v\n%s", err, job, logText(job))
	}
	list := as(platform.TestRequestInput{Path: "/bookings"})
	m := regexp.MustCompile(`href="/bookings/(\d+)">Booking #\d+</a><span>Home cleaning</span><span class="muted">` + nextWeek + ` 10:00<`).FindStringSubmatch(list.Body)
	if m == nil {
		t.Fatalf("no repeat visit on %s: %s", nextWeek, list.Body)
	}
	next := "/bookings/" + m[1]
	if o := as(platform.TestRequestInput{Path: next}); !strings.Contains(o.Body, `id="status">Confirmed<`) || !strings.Contains(o.Body, "AED 175.00") ||
		!strings.Contains(o.Body, "Pay on completion (cash)") || !strings.Contains(o.Body, "Pay by card now") || !strings.Contains(o.Body, "Every week") {
		t.Fatalf("repeat visit: %s", o.Body)
	}
	for _, want := range []string{`to fatima@mail.ae, "Your next visit is booked: Home cleaning on ` + nextWeek + `"`,
		"1 repeat booked, 1 repeat skipped", fmt.Sprintf("#%s Omar (+971501234567), Pest control (apartment), %s 16:00: window full or day closed", weekly, nextWeek)} {
		if !logged(want) {
			t.Errorf("missing in log after the daily job: %s", want)
		}
	}
	// The job handles each booking once.
	if again, _ := p.TestRequest(ctx, "bayti", platform.TestRequestInput{Job: "daily"}); !strings.HasPrefix(again.Body, "0 repeat visits booked, 0 skipped") {
		t.Fatalf("second run: %+v", again)
	}
	// The repeat visit can be paid by card from its page: nothing is left to collect.
	r = as(platform.TestRequestInput{Method: "POST", Path: next + "/pay"})
	if !strings.HasPrefix(r.Location, "/_rx/pay/") {
		t.Fatalf("pay a repeat visit: %+v", r)
	}
	req(platform.TestRequestInput{Method: "POST", Path: r.Location, Form: map[string]string{"decision": "pay"}})
	if o := as(platform.TestRequestInput{Path: next}); !strings.Contains(o.Body, "Paid by card") || strings.Contains(o.Body, "Pay by card now") {
		t.Fatalf("repeat visit after paying: %s", o.Body)
	}
	// Cancelling it together with the later visits stops the series.
	if x := as(platform.TestRequestInput{Method: "POST", Path: next + "/cancel", Form: map[string]string{"later": "1"}}); x.Location != next {
		t.Fatalf("cancel the series: %+v", x)
	}
	if again, _ := p.TestRequest(ctx, "bayti", platform.TestRequestInput{Job: "daily"}); !strings.HasPrefix(again.Body, "0 repeat visits booked, 0 skipped") {
		t.Fatalf("run after stopping the series: %+v", again)
	}

	// Arabic: the booking keeps its language for the page it returns to.
	f = form("3", "4", "3", "08:00", "cash")
	f["lang"] = "ar"
	if x := book(f); !strings.HasSuffix(x.Location, "?placed=1&lang=ar") {
		t.Fatalf("arabic booking: %+v", x)
	} else if o := as(platform.TestRequestInput{Path: x.Location}); !strings.Contains(o.Body, "تنظيف الكنب بالشامبو") || !strings.Contains(o.Body, "البرشاء") ||
		!strings.Contains(o.Body, `dir="rtl"`) || !strings.Contains(o.Body, "AED 140.00") {
		t.Fatalf("arabic booking page: %s", o.Body)
	}
}

// The owner's admin panel changes (on_admin_change), refunds, a photo of the
// problem (private upload) and repeat visits paid automatically by card
// (payments.subscribe with simulated renewals).
func TestHomeServiceUAEOwnerPhotoAndSeries(t *testing.T) {
	p := newPlatform(t)
	ctx := context.Background()
	ex, _ := examples.Get("home-service-uae")
	def, problems := appdef.Parse(ex.JSON)
	if len(problems) > 0 {
		t.Fatal(problems)
	}
	p.CreateApp("usr_test", "bayti")
	if res, err := p.ApplyChange(ctx, "bayti", def); err != nil || !res.OK {
		t.Fatal(err, res)
	}
	req := func(in platform.TestRequestInput) platform.TestResponse {
		t.Helper()
		r, err := p.TestRequest(ctx, "bayti", in)
		if err != nil {
			t.Fatalf("%+v: %v", in, err)
		}
		if r.Status >= 500 {
			t.Fatalf("%s %s: %d\n%s\n%s", in.Method, in.Path, r.Status, r.Body, logText(r))
		}
		return r
	}
	count := func(want string) int {
		t.Helper()
		logs, _ := p.Logs("bayti", logFilterAll())
		return countMessages(logs, want)
	}
	admin := func(id int64, values map[string]any) platform.TestResponse {
		t.Helper()
		return req(platform.TestRequestInput{Admin: &platform.TestAdminInput{Action: "update", Table: "bookings", ID: id, Values: values}})
	}
	for _, who := range []string{"fatima", "omar"} {
		req(platform.TestRequestInput{Method: "POST", Path: "/_rx/auth/signup", Form: map[string]string{"name": who, "email": who + "@mail.ae", "password": who + " password"}})
	}
	as := func(in platform.TestRequestInput) platform.TestResponse {
		t.Helper()
		in.UserEmail = "fatima@mail.ae"
		return req(in)
	}
	dubai := time.Now().UTC().Add(4 * time.Hour)
	visit := dubai.AddDate(0, 0, 3)
	if visit.Weekday() == time.Sunday {
		visit = visit.AddDate(0, 0, 1)
	}
	day := visit.Format("2006-01-02")
	form := func(service, qty, zone, start, payment string) map[string]string {
		return map[string]string{"service": service, "qty": qty, "zone": zone, "date": day, "slot": start, "repeat": "once", "payment": payment,
			"name": "Fatima", "phone": "+971501234567", "address": "Villa 12, Street 4"}
	}
	payCard := func(f map[string]string) string {
		t.Helper()
		r := as(platform.TestRequestInput{Method: "POST", Path: "/book", Form: f})
		if !strings.HasPrefix(r.Location, "/_rx/pay/") {
			t.Fatalf("card booking: %+v\n%s", r, logText(r))
		}
		if x := req(platform.TestRequestInput{Method: "POST", Path: r.Location, Form: map[string]string{"decision": "pay"}}); x.Status != 303 {
			t.Fatalf("pay: %+v", x)
		}
		return r.Location
	}

	// Booking 1, paid by card. The owner sets the crew in the admin panel: the booking becomes assigned and
	// the customer is emailed the crew's first name.
	payCard(form("4", "2", "1", "10:00", "card"))
	r := admin(1, map[string]any{"crew_id": 2})
	if r.Admin == nil || r.Admin.Hook != "admin_change" || !strings.Contains(r.Body, "crew Imran Khan (AC and handyman) assigned, customer emailed") {
		t.Fatalf("assign a crew: %+v\n%s", r, logText(r))
	}
	if count(`to fatima@mail.ae, "Your crew for booking #1: Imran on `+day+`"`) != 1 {
		t.Fatalf("crew email not sent:\n%s", logText(r))
	}
	if o := as(platform.TestRequestInput{Path: "/bookings/1"}); !strings.Contains(o.Body, `id="status">Crew assigned<`) || !strings.Contains(o.Body, "Imran Khan") {
		t.Fatalf("booking after the crew was set: %s", o.Body)
	}
	// The crew link from the hook's answer works; a booking whose key was cleared gets a new one.
	m := regexp.MustCompile(`/crew/1\?key=([A-Za-z0-9_-]{22})`).FindStringSubmatch(r.Body)
	if m == nil {
		t.Fatalf("no crew link: %s", r.Body)
	}
	if x := req(platform.TestRequestInput{Path: "/crew/1?key=" + m[1]}); x.Status != 200 || !strings.Contains(x.Body, "Already paid: nothing to collect") {
		t.Fatalf("crew page: %d %s", x.Status, x.Body)
	}
	if x := admin(1, map[string]any{"crew_key": ""}); !strings.Contains(x.Body, "crew key made") {
		t.Fatalf("no new crew key: %s", x.Body)
	}
	if x := req(platform.TestRequestInput{Path: "/crew/1?key=" + m[1]}); x.Status != 404 {
		t.Fatalf("old crew key still works: %d", x.Status)
	}
	// Changing another field sends nothing; a status set in the panel reaches the booking page.
	if x := admin(1, map[string]any{"notes": "Bring the ladder"}); x.Body != "nothing to do" {
		t.Fatalf("unrelated change: %s", x.Body)
	}
	if x := admin(1, map[string]any{"status": "on_the_way"}); !strings.Contains(x.Body, "status on_the_way sent to the booking page") {
		t.Fatalf("on the way from the panel: %s", x.Body)
	}
	if count(`Your crew for booking #1`) != 1 {
		t.Fatal("the crew email was sent again")
	}

	// Booking 2, paid by card, cancelled by the owner: refunded to the card and the customer is emailed.
	payCard(form("6", "1", "2", "12:00", "card"))
	if x := admin(2, map[string]any{"status": "cancelled"}); !strings.Contains(x.Body, "cancelled, refunded AED 250.00, customer emailed") {
		t.Fatalf("owner cancels: %s\n%s", x.Body, logText(x))
	}
	if count("a script refunded AED 250.00 of the payment for booking-2 (cancelled by the business)") != 1 ||
		count(`to fatima@mail.ae, "Booking #2 cancelled: Pest control (apartment) on `+day+`"`) != 1 ||
		count("AED 250.00 has been refunded to your card") != 1 {
		t.Fatal("owner cancel: refund or email missing")
	}
	if o := as(platform.TestRequestInput{Path: "/bookings/2"}); !strings.Contains(o.Body, `id="status">Cancelled<`) || !strings.Contains(o.Body, "Refunded to your card: AED 250.00") {
		t.Fatalf("booking cancelled by the owner: %s", o.Body)
	}

	// A photo of the problem: a private upload, checked to be the visitor's own before it is saved.
	upload := func(email string) string {
		t.Helper()
		x := req(platform.TestRequestInput{Method: "POST", Path: "/_rx/upload", UserEmail: email,
			Files: map[string]platform.TestFile{"file": {Name: "leak.jpg", Type: "image/jpeg", Base64: base64.StdEncoding.EncodeToString(jpegBytes(t))}}})
		var u struct{ ID string }
		if json.Unmarshal([]byte(x.Body), &u); x.Status != 200 || u.ID == "" {
			t.Fatalf("upload: %d %s", x.Status, x.Body)
		}
		return u.ID
	}
	mine, omars := upload("fatima@mail.ae"), upload("omar@mail.ae")
	if x := as(platform.TestRequestInput{Path: "/checkout?service=5&qty=1&zone=1&date=" + day + "&slot=14:00&repeat=once"}); !strings.Contains(x.Body, `<script src="/_rx/upload.js">`) ||
		!strings.Contains(x.Body, `name="photo" data-rx-upload`) {
		t.Fatalf("checkout without the photo field: %s", x.Body)
	}
	f := form("5", "1", "1", "14:00", "cash")
	f["photo"] = omars
	if x := as(platform.TestRequestInput{Method: "POST", Path: "/book", Form: f}); !strings.HasSuffix(x.Location, "&error=error_photo") {
		t.Fatalf("someone else's photo: %+v", x)
	}
	f["photo"] = mine
	x := as(platform.TestRequestInput{Method: "POST", Path: "/book", Form: f})
	if x.Location != "/bookings/3?placed=1" {
		t.Fatalf("booking with a photo: %+v\n%s", x, logText(x))
	}
	if count(`"New booking #3 (cash): Handyman, `+day+` 14:00, Dubai Marina, AED 110.00, photo attached"`) != 1 ||
		count("Photo of the problem: attached, open booking #3 in the admin panel") != 1 {
		t.Fatal("owner not told about the photo")
	}
	if o := as(platform.TestRequestInput{Path: "/bookings/3"}); !strings.Contains(o.Body, `<img src="/_rx/private/`+mine+`"`) {
		t.Fatalf("customer does not see the photo: %s", o.Body)
	}
	logs, _ := p.Logs("bayti", logFilterAll())
	var key3 string
	for _, e := range logs {
		if m := regexp.MustCompile(`/crew/3\?key=([A-Za-z0-9_-]{22})`).FindStringSubmatch(e.Message); m != nil {
			key3 = m[1]
		}
	}
	if c := req(platform.TestRequestInput{Path: "/crew/3?key=" + key3}); c.Status != 200 || !strings.Contains(c.Body, "Photo attached: see the admin panel") ||
		strings.Contains(c.Body, "/_rx/private/") {
		t.Fatalf("crew page with a photo: %d %s", c.Status, c.Body)
	}

	// Repeat visits paid automatically: the owner allows booking 30 days ahead so that the next two weekly visits fit.
	if x := req(platform.TestRequestInput{Admin: &platform.TestAdminInput{Action: "update", Table: "business", ID: 1, Values: map[string]any{"days_ahead": 30}}}); x.Body != "nothing to do" {
		t.Fatalf("business settings: %+v", x)
	}
	q := "/checkout?service=1&qty=3&zone=5&date=" + day + "&slot=16:00&repeat=weekly"
	if x := as(platform.TestRequestInput{Path: q}); !strings.Contains(x.Body, `value="auto"> Pay automatically: card charged for each visit, every week`) {
		t.Fatalf("no automatic payment offered: %s", x.Body)
	}
	if x := as(platform.TestRequestInput{Path: strings.Replace(q, "weekly", "once", 1)}); strings.Contains(x.Body, `value="auto"`) {
		t.Fatal("automatic payment offered for one visit")
	}
	f = form("1", "3", "5", "16:00", "auto")
	f["repeat"] = "weekly"
	sub := as(platform.TestRequestInput{Method: "POST", Path: "/book", Form: f})
	if !strings.HasPrefix(sub.Location, "/_rx/pay/rxs_") {
		t.Fatalf("subscribe: %+v\n%s", sub, logText(sub))
	}
	if pg := req(platform.TestRequestInput{Path: sub.Location}); !strings.Contains(pg.Body, "145.00 AED every week") {
		t.Fatalf("subscription page: %s", pg.Body)
	}
	if o := as(platform.TestRequestInput{Path: "/bookings/4"}); !strings.Contains(o.Body, "Waiting for payment") || strings.Contains(o.Body, "Pay by card now") {
		t.Fatalf("series waiting for its first payment: %s", o.Body)
	}
	if x := req(platform.TestRequestInput{Method: "POST", Path: sub.Location, Form: map[string]string{"decision": "pay"}}); x.Location != "/bookings/4?placed=1" {
		t.Fatalf("first payment: %+v", x)
	}
	if o := as(platform.TestRequestInput{Path: "/bookings/4"}); !strings.Contains(o.Body, `id="status">Confirmed<`) || !strings.Contains(o.Body, "Paid by card") ||
		!strings.Contains(o.Body, "Paid automatically: your card is charged for each visit") {
		t.Fatalf("first visit of the series: %s", o.Body)
	}
	visitOn := func(date string) string {
		t.Helper()
		list := as(platform.TestRequestInput{Path: "/bookings"})
		m := regexp.MustCompile(`href="/bookings/(\d+)">Booking #\d+</a><span>Home cleaning</span><span class="muted">` + date + ` 16:00<`).FindStringSubmatch(list.Body)
		if m == nil {
			t.Fatalf("no visit on %s: %s", date, list.Body)
		}
		return "/bookings/" + m[1]
	}
	week1, week2 := visit.AddDate(0, 0, 7).Format("2006-01-02"), visit.AddDate(0, 0, 14).Format("2006-01-02")

	// A renewal before the daily job ran books next week's visit and pays it.
	req(platform.TestRequestInput{Method: "POST", Path: sub.Location, Form: map[string]string{"decision": "renew"}})
	second := visitOn(week1)
	if o := as(platform.TestRequestInput{Path: second}); !strings.Contains(o.Body, "Paid by card") || strings.Contains(o.Body, "Pay by card now") || !strings.Contains(o.Body, "AED 145.00") {
		t.Fatalf("visit paid by the renewal: %s", o.Body)
	}
	if count(`to fatima@mail.ae, "Your next visit is booked: Home cleaning on `+week1+`"`) != 1 {
		t.Fatal("customer not told about the next visit")
	}
	// The daily job books the visit after it, waiting for its charge: nothing to pay by hand or to the crew.
	if job := req(platform.TestRequestInput{Job: "daily"}); !strings.HasPrefix(job.Body, "1 repeat visits booked, 0 skipped") {
		t.Fatalf("daily job: %+v\n%s", job, logText(job))
	}
	third := visitOn(week2)
	if o := as(platform.TestRequestInput{Path: third}); !strings.Contains(o.Body, "Card, charged automatically") || strings.Contains(o.Body, "Pay by card now") {
		t.Fatalf("visit waiting for its charge: %s", o.Body)
	}
	if count("Your card is charged automatically: nothing to pay the crew.") != 2 {
		t.Fatal("repeat emails for the series")
	}
	// A failed renewal: that visit is paid on completion or by card, and the customer gets a link, once.
	req(platform.TestRequestInput{Method: "POST", Path: sub.Location, Form: map[string]string{"decision": "renewal_failed"}})
	req(platform.TestRequestInput{Method: "POST", Path: sub.Location, Form: map[string]string{"decision": "renewal_failed"}})
	if o := as(platform.TestRequestInput{Path: third}); !strings.Contains(o.Body, "Pay on completion (cash)") || !strings.Contains(o.Body, "Pay by card now") {
		t.Fatalf("visit after a failed renewal: %s", o.Body)
	}
	if n := count(`to fatima@mail.ae, "Card payment failed for your visit on ` + week2 + `"`); n != 1 {
		t.Fatalf("payment failed emails: %d", n)
	}
	if count("https://bayti--preview.renoxium.test"+third) == 0 {
		t.Fatal("no pay link in the email")
	}
	// Cancelling next week's visit and all later ones refunds it and stops the automatic payments.
	if x := as(platform.TestRequestInput{Method: "POST", Path: second + "/cancel", Form: map[string]string{"later": "1"}}); x.Location != second {
		t.Fatalf("cancel the series: %+v\n%s", x, logText(x))
	}
	if count("a script refunded AED 145.00 of the payment for series-4 (cancelled by the customer)") != 1 {
		t.Fatal("series visit not refunded")
	}
	for _, path := range []string{second, third} {
		if o := as(platform.TestRequestInput{Path: path}); !strings.Contains(o.Body, `id="status">Cancelled<`) {
			t.Fatalf("%s not cancelled: %s", path, o.Body)
		}
	}
	if o := as(platform.TestRequestInput{Path: second}); !strings.Contains(o.Body, "Refunded to your card: AED 145.00") {
		t.Fatalf("refund not shown: %s", o.Body)
	}
	if x := req(platform.TestRequestInput{Method: "POST", Path: sub.Location, Form: map[string]string{"decision": "renew"}}); x.Status < 400 {
		t.Fatalf("renewal after the series was cancelled: %+v", x)
	}
	if job := req(platform.TestRequestInput{Job: "daily"}); !strings.HasPrefix(job.Body, "0 repeat visits booked, 0 skipped") {
		t.Fatalf("daily job after the series stopped: %+v", job)
	}
}

func TestHomeServiceUAETools(t *testing.T) {
	p := newPlatform(t)
	ctx := context.Background()
	ex, _ := examples.Get("home-service-uae")
	def, problems := appdef.Parse(ex.JSON)
	if len(problems) > 0 {
		t.Fatal(problems)
	}
	p.CreateApp("usr_test", "bayti")
	if res, err := p.ApplyChange(ctx, "bayti", def); err != nil || !res.OK {
		t.Fatal(err, res)
	}
	call := func(name string, args map[string]any) (string, bool) {
		t.Helper()
		r, err := p.TestRequest(ctx, "bayti", platform.TestRequestInput{Method: "POST", Path: "/mcp",
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
		if len(rpc.Result.Content) == 0 {
			t.Fatalf("%s: %s", name, r.Body)
		}
		return rpc.Result.Content[0].Text, rpc.Result.IsError
	}
	if out, bad := call("list_services", map[string]any{"category": "cleaning"}); bad || !strings.Contains(out, `"name":"Home cleaning"`) ||
		!strings.Contains(out, `"priced":"per_hour"`) || strings.Contains(out, "AC service") || !strings.Contains(out, `"url":"https://bayti--preview.renoxium.test/s/1"`) {
		t.Errorf("list_services: %s", out)
	}
	// The same price as the booking form: 3 x 40 + 3 x 10 materials + 25 surcharge.
	if out, bad := call("get_quote", map[string]any{"service_id": 1, "quantity": 3, "area": "arabian ranches", "materials": true}); bad ||
		!strings.Contains(out, `"subtotal":150`) || !strings.Contains(out, `"area_surcharge":25`) || !strings.Contains(out, `"total":175`) || !strings.Contains(out, `"vat_included":8.33`) {
		t.Errorf("get_quote: %s", out)
	}
	if out, bad := call("get_quote", map[string]any{"service_id": 1, "quantity": 3, "area": "Sharjah"}); !bad || !strings.Contains(out, "do not serve") {
		t.Errorf("quote outside the areas served: %s", out)
	}
	day := time.Now().UTC().Add(4*time.Hour).AddDate(0, 0, 3)
	if day.Weekday() == time.Sunday {
		day = day.AddDate(0, 0, 1)
	}
	if out, bad := call("check_availability", map[string]any{"date": day.Format("2006-01-02")}); bad || !strings.Contains(out, `"bookable":true`) ||
		!strings.Contains(out, `{"arrival_from":"08:00","arrival_until":"10:00","crews_free":2}`) {
		t.Errorf("check_availability: %s", out)
	}
	if out, bad := call("check_availability", map[string]any{"date": "2020-01-01"}); bad || !strings.Contains(out, `"bookable":false`) || !strings.Contains(out, "in the past") {
		t.Errorf("availability in the past: %s", out)
	}
	if out, bad := call("business_info", map[string]any{"lang": "ar"}); bad || !strings.Contains(out, "بيتي للخدمات المنزلية") ||
		!strings.Contains(out, `"payment_methods":["card","cash on completion"]`) || !strings.Contains(out, `"closed_on":"sunday"`) {
		t.Errorf("business_info: %s", out)
	}
	if r, err := p.TestRequest(ctx, "bayti", platform.TestRequestInput{Job: "daily"}); err != nil || r.Status != 200 ||
		r.Body != "0 repeat visits booked, 0 skipped, 0 reminders, 0 visits tomorrow" {
		t.Errorf("daily job without bookings: %v %+v", err, r)
	}
}
