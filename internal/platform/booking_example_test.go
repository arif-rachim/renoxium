package platform_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/arif-rachim/renoxium/examples"
	"github.com/arif-rachim/renoxium/internal/appdef"
	"github.com/arif-rachim/renoxium/internal/platform"
)

// The UAE booking example works end to end on preview: browsing in two
// languages, free times, an appointment paid by card (simulated) and one paid
// at the venue, no double booking, classes that fill up, a failed payment
// that frees its time, bookings private to their customer, cancelling, the
// evening job and the tools for AI assistants.
func TestBookingUAEExample(t *testing.T) {
	p := newPlatform(t)
	ctx := context.Background()
	ex, ok := examples.Get("booking-uae")
	if !ok {
		t.Fatal("example not registered")
	}
	def, problems := appdef.Parse(ex.JSON)
	if len(problems) > 0 {
		t.Fatal(problems)
	}
	p.CreateApp("usr_test", "lamsa")
	res, err := p.ApplyChange(ctx, "lamsa", def)
	if err != nil || !res.OK {
		t.Fatal(err, res)
	}
	req := func(in platform.TestRequestInput) platform.TestResponse {
		t.Helper()
		r, err := p.TestRequest(ctx, "lamsa", in)
		if err != nil {
			t.Fatalf("%+v: %v", in, err)
		}
		if r.Status >= 500 {
			t.Fatalf("%s %s: %d\n%s", in.Method, in.Path, r.Status, logText(r))
		}
		return r
	}
	// Tomorrow in the UAE (UTC+4): every time of that day is still ahead, and it is far enough away to cancel.
	tomorrow := time.Now().UTC().Add(4*time.Hour).AddDate(0, 0, 1).Format("2006-01-02")
	free := func(service, staff, clock string) bool {
		t.Helper()
		r := req(platform.TestRequestInput{Path: "/api/slots?service=" + service + "&staff=" + staff + "&date=" + tomorrow})
		return strings.Contains(r.Body, `"time":"`+clock+`"`)
	}

	home := req(platform.TestRequestInput{Path: "/"})
	for _, want := range []string{`dir="ltr"`, "Lamsa Beauty Lounge", "Haircut &amp; Blow-dry", "AED 150.00", "Face &amp; Body", "Kids&#39; Braiding Club", "3 seats left", "Open 10:00 to 21:00"} {
		if !strings.Contains(home.Body, want) {
			t.Errorf("home lacks %q", want)
		}
	}
	ar := req(platform.TestRequestInput{Path: "/?lang=ar"})
	for _, want := range []string{`dir="rtl"`, "لمسة بيوتي لاونج", "قص الشعر مع سشوار", "الأظافر", "نادي تضفير الشعر للأطفال"} {
		if !strings.Contains(ar.Body, want) {
			t.Errorf("arabic home lacks %q", want)
		}
	}
	if r := req(platform.TestRequestInput{Path: "/s/1"}); r.Status != 200 || !strings.Contains(r.Body, "Includes VAT") || !strings.Contains(r.Body, `<input type="radio" name="staff" value="1"><span>Layla</span>`) ||
		strings.Contains(r.Body, "Maryam") || !strings.Contains(r.Body, "Sign in to book") {
		t.Fatalf("service page: %d %s", r.Status, r.Body)
	}
	if r := req(platform.TestRequestInput{Path: "/s/999"}); r.Status != 404 {
		t.Fatalf("missing service: %d", r.Status)
	}
	if r := req(platform.TestRequestInput{Path: "/c/999"}); r.Status != 404 {
		t.Fatalf("missing class: %d", r.Status)
	}

	// Free times follow the opening hours (10:00 to 21:00) and the service's length (60 minutes).
	if !free("1", "1", "10:00") || !free("1", "1", "20:00") || free("1", "1", "20:30") || free("1", "1", "09:30") {
		t.Fatal("slots do not follow the opening hours")
	}
	if r := req(platform.TestRequestInput{Path: "/api/slots?service=1&date=2020-01-01"}); !strings.Contains(r.Body, `"slots":[]`) {
		t.Fatalf("a past date has slots: %s", r.Body)
	}
	if r := req(platform.TestRequestInput{Path: "/api/slots?service=1&date=nonsense"}); r.Status != 200 || !strings.Contains(r.Body, `"slots":[]`) {
		t.Fatalf("a bad date: %+v", r)
	}

	appt := func(service, staff, clock, payment string) map[string]string {
		return map[string]string{"service": service, "staff": staff, "date": tomorrow, "time": clock, "name": "Fatima", "phone": "+971501234567", "payment": payment}
	}
	// Signed out: booking sends the visitor to sign in and back to the service.
	if r := req(platform.TestRequestInput{Method: "POST", Path: "/book", Form: appt("1", "1", "10:00", "card")}); r.Status != 303 || !strings.HasPrefix(r.Location, "/_rx/auth/login?next=") {
		t.Fatalf("signed out booking: %+v", r)
	}
	req(platform.TestRequestInput{Method: "POST", Path: "/_rx/auth/signup", Form: map[string]string{"name": "Fatima", "email": "fatima@mail.ae", "password": "fatima password"}})
	req(platform.TestRequestInput{Method: "POST", Path: "/_rx/auth/signup", Form: map[string]string{"name": "Other", "email": "other@mail.ae", "password": "other password"}})
	as := func(in platform.TestRequestInput) platform.TestResponse {
		t.Helper()
		in.UserEmail = "fatima@mail.ae"
		return req(in)
	}
	other := func(in platform.TestRequestInput) platform.TestResponse {
		t.Helper()
		in.UserEmail = "other@mail.ae"
		return req(in)
	}
	if r := as(platform.TestRequestInput{Path: "/s/1"}); !strings.Contains(r.Body, `value="Fatima"`) || !strings.Contains(r.Body, "Pay a deposit by card now: AED 75.00") ||
		!strings.Contains(r.Body, "Book now, pay at the venue") {
		t.Fatalf("booking form for a signed-in visitor: %s", r.Body)
	}

	// Card: a 50% deposit of AED 150. The time is held while the payment is open.
	r := as(platform.TestRequestInput{Method: "POST", Path: "/book", Form: appt("1", "1", "10:00", "card")})
	if r.Status != 303 || !strings.HasPrefix(r.Location, "/_rx/pay/") {
		t.Fatalf("card booking: %+v", r)
	}
	if !strings.Contains(req(platform.TestRequestInput{Path: r.Location}).Body, "75.00 AED") {
		t.Fatal("charged amount")
	}
	if free("1", "1", "10:00") || free("1", "1", "10:30") || !free("1", "1", "11:00") {
		t.Fatal("a booking waiting for payment does not hold its time")
	}
	paid := req(platform.TestRequestInput{Method: "POST", Path: r.Location, Form: map[string]string{"decision": "pay"}})
	if paid.Location != "/bookings/1?placed=1" {
		t.Fatalf("after paying: %+v", paid)
	}
	page := as(platform.TestRequestInput{Path: "/bookings/1?placed=1"})
	for _, want := range []string{"<strong>Confirmed</strong>", "Thank you", "Haircut &amp; Blow-dry", tomorrow, "10:00 – 11:00", "Layla", "AED 150.00", "AED 75.00", "AED 7.14", "Cancel booking"} {
		if !strings.Contains(page.Body, want) {
			t.Errorf("booking page lacks %q", want)
		}
	}
	if free("1", "1", "10:00") {
		t.Fatal("the paid time is still offered")
	}

	// The same time with the same staff member is refused, whatever the browser sends.
	if r := other(platform.TestRequestInput{Method: "POST", Path: "/book", Form: appt("1", "1", "10:00", "venue")}); r.Location != "/s/1?error=error_slot" {
		t.Fatalf("double booking: %+v", r)
	}
	if r := other(platform.TestRequestInput{Method: "POST", Path: "/book", Form: appt("2", "1", "09:30", "venue")}); r.Location != "/s/2?error=error_slot" {
		t.Fatalf("booking before opening: %+v", r)
	}
	if r := as(platform.TestRequestInput{Path: "/s/1?error=error_slot"}); !strings.Contains(r.Body, "no longer available") {
		t.Fatal("slot error message")
	}
	f := appt("1", "1", "12:00", "venue")
	f["phone"] = ""
	if r := as(platform.TestRequestInput{Method: "POST", Path: "/book", Form: f}); r.Location != "/s/1?error=error_fields" {
		t.Fatalf("missing phone: %+v", r)
	}

	// Pay at the venue, anyone available: confirmed at once with the first free staff member, nothing paid online.
	r = as(platform.TestRequestInput{Method: "POST", Path: "/book", Form: appt("3", "", "11:00", "venue")})
	if r.Location != "/bookings/2?placed=1" {
		t.Fatalf("venue booking: %+v", r)
	}
	if o := as(platform.TestRequestInput{Path: "/bookings/2"}); !strings.Contains(o.Body, "<strong>Confirmed</strong>") || !strings.Contains(o.Body, "Book now, pay at the venue") ||
		!strings.Contains(o.Body, "Maryam") || !strings.Contains(o.Body, "AED 70.00") || !strings.Contains(o.Body, "AED 0.00") {
		t.Fatalf("venue booking page: %s", o.Body)
	}
	if free("3", "2", "11:00") || !free("3", "3", "11:00") || !free("3", "", "11:00") {
		t.Fatal("anyone available: only the chosen staff member is busy")
	}

	// Preview never sends email: confirmations and the owner notices are in the log.
	logs, _ := p.Logs("lamsa", logFilterAll())
	for _, want := range []string{`not sent from preview: to fatima@mail.ae, "Your booking #1 at Lamsa Beauty Lounge"`,
		"https://lamsa--preview.renoxium.test/bookings/2", `to the owner, "New booking #1: Haircut & Blow-dry, `, `, Layla, AED 75.00 paid by card"`,
		`to the owner, "New booking #2: Classic Manicure, `, `, Maryam, pay at venue AED 70.00"`} {
		if countMessages(logs, want) == 0 {
			t.Errorf("missing email in log: %s", want)
		}
	}

	// A failed card payment frees the time.
	r = as(platform.TestRequestInput{Method: "POST", Path: "/book", Form: appt("1", "1", "14:00", "card")})
	if !strings.HasPrefix(r.Location, "/_rx/pay/") || free("1", "1", "14:00") {
		t.Fatalf("time before failure: %+v", r)
	}
	req(platform.TestRequestInput{Method: "POST", Path: r.Location, Form: map[string]string{"decision": "fail"}})
	if !free("1", "1", "14:00") {
		t.Fatal("time not freed after a failed payment")
	}
	if o := as(platform.TestRequestInput{Path: "/bookings/3"}); !strings.Contains(o.Body, "Payment failed") || strings.Contains(o.Body, "Cancel booking") {
		t.Fatal("failed booking page")
	}
	if r := as(platform.TestRequestInput{Path: "/bookings/3/receipt"}); r.Status != 404 {
		t.Fatalf("receipt of an unpaid booking: %d", r.Status)
	}

	// Classes: three seats. A failed payment gives its seat back; when full, enrolment is refused.
	enrol := func(participant, payment string) map[string]string {
		return map[string]string{"class": "2", "participant": participant, "name": "Fatima", "phone": "+971501234567", "payment": payment}
	}
	if r := as(platform.TestRequestInput{Method: "POST", Path: "/enrol", Form: enrol("", "venue")}); r.Location != "/c/2?error=error_participant" {
		t.Fatalf("enrolment without a participant: %+v", r)
	}
	for i, child := range []string{"Sara", "Noura"} {
		if r := as(platform.TestRequestInput{Method: "POST", Path: "/enrol", Form: enrol(child, "venue")}); !strings.HasPrefix(r.Location, "/bookings/") || !strings.HasSuffix(r.Location, "?placed=1") {
			t.Fatalf("enrolment %d: %+v", i, r)
		}
	}
	if c := req(platform.TestRequestInput{Path: "/c/2"}); !strings.Contains(c.Body, "1 seat left") || !strings.Contains(c.Body, "Ages 8 to 12") || !strings.Contains(c.Body, "Every Sunday") {
		t.Fatalf("class page: %s", c.Body)
	}
	r = other(platform.TestRequestInput{Method: "POST", Path: "/enrol", Form: enrol("Hind", "card")})
	if !strings.Contains(req(platform.TestRequestInput{Path: r.Location}).Body, "225.00 AED") {
		t.Fatalf("class deposit: %+v", r)
	}
	if r := as(platform.TestRequestInput{Method: "POST", Path: "/enrol", Form: enrol("Late", "venue")}); r.Location != "/c/2?error=error_full" {
		t.Fatalf("a seat waiting for payment was given away: %+v", r)
	}
	req(platform.TestRequestInput{Method: "POST", Path: r.Location, Form: map[string]string{"decision": "fail"}})
	if r := other(platform.TestRequestInput{Method: "POST", Path: "/enrol", Form: enrol("Hind", "venue")}); !strings.HasPrefix(r.Location, "/bookings/") {
		t.Fatalf("seat not freed after a failed payment: %+v", r)
	}
	if r := as(platform.TestRequestInput{Method: "POST", Path: "/enrol", Form: enrol("Late", "venue")}); r.Location != "/c/2?error=error_full" {
		t.Fatalf("enrolment in a full class: %+v", r)
	}
	if c := as(platform.TestRequestInput{Path: "/c/2?error=error_full"}); !strings.Contains(c.Body, "Fully booked") || !strings.Contains(c.Body, "this class is fully booked") ||
		strings.Contains(c.Body, `action="/enrol"`) {
		t.Fatal("full class page")
	}
	if o := as(platform.TestRequestInput{Path: "/bookings/4"}); !strings.Contains(o.Body, "Kids&#39; Braiding Club") || !strings.Contains(o.Body, "Sara") || !strings.Contains(o.Body, "AED 450.00") {
		t.Fatalf("enrolment page: %s", o.Body)
	}

	// Receipt for the paid appointment: AED 150 incl. VAT = 142.86 + 7.14; 75 paid online, 75 at the venue.
	rec := as(platform.TestRequestInput{Path: "/bookings/1/receipt"})
	for _, want := range []string{"<h1>Receipt</h1>", "Lamsa Beauty Lounge LLC", ">142.86<", ">7.14<", "AED 150.00", "AED 75.00", "Fatima", "Layla"} {
		if !strings.Contains(rec.Body, want) {
			t.Errorf("receipt lacks %q", want)
		}
	}

	// Bookings are private: another visitor sees neither the list entry nor the page, and cannot cancel.
	if r := other(platform.TestRequestInput{Path: "/bookings/1"}); r.Status != 404 {
		t.Fatalf("someone else's booking: %d", r.Status)
	}
	if r := other(platform.TestRequestInput{Path: "/bookings/1/receipt"}); r.Status != 404 {
		t.Fatalf("someone else's receipt: %d", r.Status)
	}
	if r := other(platform.TestRequestInput{Method: "POST", Path: "/bookings/1/cancel"}); r.Status != 404 {
		t.Fatalf("cancelling someone else's booking: %+v", r)
	}
	if r := other(platform.TestRequestInput{Path: "/bookings"}); strings.Contains(r.Body, ">Booking #1<") || strings.Contains(r.Body, "Haircut") || !strings.Contains(r.Body, "Hind") {
		t.Fatal("someone else's booking listed")
	}
	if r := as(platform.TestRequestInput{Path: "/bookings"}); !strings.Contains(r.Body, ">Booking #1<") || !strings.Contains(r.Body, ">Booking #2<") || strings.Contains(r.Body, "Hind") {
		t.Fatal("own bookings list")
	}
	if r := req(platform.TestRequestInput{Path: "/bookings"}); r.Status != 303 || !strings.HasPrefix(r.Location, "/_rx/auth/login") {
		t.Fatalf("signed out bookings: %+v", r)
	}

	// The evening job: tomorrow's schedule to the owner and one reminder per customer booking (logged on preview).
	job, err := p.TestRequest(ctx, "lamsa", platform.TestRequestInput{Job: "daily_schedule"})
	if err != nil || job.Status != 200 || !strings.HasPrefix(job.Body, "2 appointments and ") || !strings.HasSuffix(job.Body, "; 0 unpaid bookings expired") {
		t.Fatalf("daily schedule: %v %+v\n%s", err, job, logText(job))
	}
	logs, _ = p.Logs("lamsa", logFilterAll())
	for _, want := range []string{`not sent from preview: to the owner, "Tomorrow: 2 appointments and `, "10:00-11:00  Haircut & Blow-dry with Layla: Fatima, +971501234567 (to pay at the venue: AED 75.00)",
		`not sent from preview: to fatima@mail.ae, "Reminder: Haircut & Blow-dry tomorrow at 10:00"`, `to fatima@mail.ae, "Reminder: Classic Manicure tomorrow at 11:00"`} {
		if !anyLog(logs, want) {
			t.Errorf("daily schedule: missing in log: %s", want)
		}
	}
	if again, _ := p.TestRequest(ctx, "lamsa", platform.TestRequestInput{Job: "daily_schedule"}); !strings.Contains(again.Body, "; 0 reminders sent; ") {
		t.Fatalf("reminders sent twice: %+v", again)
	}

	// The customer cancels (tomorrow is further away than the 4-hour limit): the time is free again.
	if r := as(platform.TestRequestInput{Method: "POST", Path: "/bookings/2/cancel"}); r.Location != "/bookings/2" {
		t.Fatalf("cancel: %+v", r)
	}
	if o := as(platform.TestRequestInput{Path: "/bookings/2"}); !strings.Contains(o.Body, "<strong>Cancelled</strong>") || strings.Contains(o.Body, "Cancel booking") {
		t.Fatal("cancelled booking page")
	}
	if !free("3", "2", "11:00") {
		t.Fatal("time not freed after cancelling")
	}
	if r := as(platform.TestRequestInput{Method: "POST", Path: "/bookings/2/cancel"}); r.Location != "/bookings/2?error=error_cancel" {
		t.Fatalf("cancelling twice: %+v", r)
	}
	if logs, _ := p.Logs("lamsa", logFilterAll()); !anyLog(logs, `to the owner, "Cancelled by customer #2: Classic Manicure, `) {
		t.Fatal("owner not told about the cancellation")
	}
}

func TestBookingUAETools(t *testing.T) {
	p := newPlatform(t)
	ctx := context.Background()
	ex, _ := examples.Get("booking-uae")
	def, problems := appdef.Parse(ex.JSON)
	if len(problems) > 0 {
		t.Fatal(problems)
	}
	p.CreateApp("usr_test", "lamsa")
	if res, err := p.ApplyChange(ctx, "lamsa", def); err != nil || !res.OK {
		t.Fatal(err, res)
	}
	call := func(name string, args map[string]any) (string, bool) {
		t.Helper()
		r, err := p.TestRequest(ctx, "lamsa", platform.TestRequestInput{Method: "POST", Path: "/mcp",
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
			t.Fatalf("%s failed: %s", name, r.Body)
		}
		return rpc.Result.Content[0].Text, !rpc.Result.IsError
	}
	tomorrow := time.Now().UTC().Add(4*time.Hour).AddDate(0, 0, 1).Format("2006-01-02")

	if out, ok := call("list_services", nil); !ok || !strings.Contains(out, `"name":"Root Colour","price":280`) ||
		!strings.Contains(out, `"url":"https://lamsa--preview.renoxium.test/s/1"`) || !strings.Contains(out, `"seats_left":3`) || !strings.Contains(out, `"price_for_term":450`) {
		t.Errorf("list_services: %s", out)
	}
	if out, ok := call("list_services", map[string]any{"lang": "ar"}); !ok || !strings.Contains(out, "حمام مغربي") || !strings.Contains(out, `"weekday":"الأحد"`) {
		t.Errorf("list_services in Arabic: %s", out)
	}
	if out, ok := call("business_info", nil); !ok || !strings.Contains(out, `"payment_methods":["card","pay at the venue"]`) || !strings.Contains(out, `"online_deposit_percent":50`) ||
		!strings.Contains(out, `"opens":"10:00"`) {
		t.Errorf("business_info: %s", out)
	}
	if out, ok := call("check_availability", map[string]any{"service": 2, "date": tomorrow}); !ok || !strings.Contains(out, `"free_times":["10:00","10:30","11:00"`) ||
		!strings.Contains(out, `"19:30"]`) || !strings.Contains(out, `"book_at":"https://lamsa--preview.renoxium.test/s/2"`) {
		t.Errorf("check_availability: %s", out)
	}
	if out, ok := call("check_availability", map[string]any{"service": 99, "date": tomorrow}); ok {
		t.Errorf("availability of a missing service: %s", out)
	}

	// A booking takes its time out of the tool's answer: Root Colour (90 minutes) is done only by Layla.
	p.TestRequest(ctx, "lamsa", platform.TestRequestInput{Method: "POST", Path: "/_rx/auth/signup", Form: map[string]string{"name": "Fatima", "email": "fatima@mail.ae", "password": "fatima password"}})
	r, err := p.TestRequest(ctx, "lamsa", platform.TestRequestInput{Method: "POST", Path: "/book", UserEmail: "fatima@mail.ae",
		Form: map[string]string{"service": "1", "staff": "1", "date": tomorrow, "time": "10:00", "name": "Fatima", "phone": "+971501234567", "payment": "venue"}})
	if err != nil || r.Location != "/bookings/1?placed=1" {
		t.Fatalf("booking: %v %+v", err, r)
	}
	if out, ok := call("check_availability", map[string]any{"service": 2, "date": tomorrow}); !ok || !strings.Contains(out, `"free_times":["11:00","11:30"`) {
		t.Errorf("check_availability after a booking: %s", out)
	}
	if r, err := p.TestRequest(ctx, "lamsa", platform.TestRequestInput{Job: "daily_schedule"}); err != nil || r.Status != 200 || !strings.HasPrefix(r.Body, "1 appointments and ") {
		t.Errorf("daily schedule: %v %+v", err, r)
	}
}

// Money after booking in the UAE booking example, on preview: a customer's
// cancellation refunds the card, the owner's changes in the admin panel
// (test_request admin) refund and email the customer, a moved booking is
// checked for double booking, and a monthly class runs on a subscription
// that renews, fails, is stopped by the customer and ends.
func TestBookingUAERefundsAndSubscriptions(t *testing.T) {
	p := newPlatform(t)
	ctx := context.Background()
	ex, _ := examples.Get("booking-uae")
	def, problems := appdef.Parse(ex.JSON)
	if len(problems) > 0 {
		t.Fatal(problems)
	}
	p.CreateApp("usr_test", "lamsa")
	if res, err := p.ApplyChange(ctx, "lamsa", def); err != nil || !res.OK {
		t.Fatal(err, res)
	}
	req := func(in platform.TestRequestInput) platform.TestResponse {
		t.Helper()
		r, err := p.TestRequest(ctx, "lamsa", in)
		if err != nil {
			t.Fatalf("%+v: %v", in, err)
		}
		if r.Status >= 500 {
			t.Fatalf("%s %s: %d\n%s", in.Method, in.Path, r.Status, logText(r))
		}
		return r
	}
	user := "fatima@mail.ae"
	as := func(in platform.TestRequestInput) platform.TestResponse {
		t.Helper()
		in.UserEmail = user
		return req(in)
	}
	admin := func(id int64, values map[string]any) platform.TestResponse {
		t.Helper()
		r, err := p.TestRequest(ctx, "lamsa", platform.TestRequestInput{Admin: &platform.TestAdminInput{Action: "update", Table: "bookings", ID: id, Values: values, By: "owner@lamsa.ae"}})
		if err != nil || r.Status != 200 || r.Admin == nil {
			t.Fatalf("admin %d %v: %v %+v\n%s", id, values, err, r, logText(r))
		}
		return r
	}
	logged := func(text string) bool {
		t.Helper()
		logs, _ := p.Logs("lamsa", logFilterAll())
		return anyLog(logs, text)
	}
	tomorrow := time.Now().UTC().Add(4*time.Hour).AddDate(0, 0, 1).Format("2006-01-02")
	free := func(service, staff, clock string) bool {
		t.Helper()
		r := req(platform.TestRequestInput{Path: "/api/slots?service=" + service + "&staff=" + staff + "&date=" + tomorrow})
		return strings.Contains(r.Body, `"time":"`+clock+`"`)
	}
	req(platform.TestRequestInput{Method: "POST", Path: "/_rx/auth/signup", Form: map[string]string{"name": "Fatima", "email": "fatima@mail.ae", "password": "fatima password"}})
	req(platform.TestRequestInput{Method: "POST", Path: "/_rx/auth/signup", Form: map[string]string{"name": "Mona", "email": "mona@mail.ae", "password": "mona password"}})
	book := func(service, staff, clock, payment string) {
		t.Helper()
		r := as(platform.TestRequestInput{Method: "POST", Path: "/book", Form: map[string]string{"service": service, "staff": staff, "date": tomorrow, "time": clock,
			"name": "Fatima", "phone": "+971501234567", "payment": payment}})
		if r.Status != 303 || strings.Contains(r.Location, "error=") {
			t.Fatalf("booking %s %s: %+v", service, clock, r)
		}
		if strings.HasPrefix(r.Location, "/_rx/pay/") {
			if paid := req(platform.TestRequestInput{Method: "POST", Path: r.Location, Form: map[string]string{"decision": "pay"}}); !strings.HasPrefix(paid.Location, "/bookings/") {
				t.Fatalf("paying %s: %+v", r.Location, paid)
			}
		}
	}

	// The customer cancels a card booking in time: the AED 75 deposit goes back to the card at once and the booking stays cancelled.
	book("1", "1", "10:00", "card")
	if r := as(platform.TestRequestInput{Method: "POST", Path: "/bookings/1/cancel"}); r.Location != "/bookings/1" {
		t.Fatalf("cancel: %+v", r)
	}
	page := as(platform.TestRequestInput{Path: "/bookings/1"})
	for _, want := range []string{"<strong>Cancelled</strong>", "<span>Refunded</span><span>AED 75.00</span>", "<span>Paid online</span><span>AED 0.00</span>"} {
		if !strings.Contains(page.Body, want) {
			t.Errorf("cancelled booking page lacks %q", want)
		}
	}
	if !free("1", "1", "10:00") {
		t.Error("time not freed after the cancellation")
	}
	for _, want := range []string{"a script refunded AED 75.00 of the payment for booking-1", `to fatima@mail.ae, "Booking #1 at Lamsa Beauty Lounge is cancelled"`,
		"AED 75.00 has been refunded to your card", `to the owner, "Cancelled by customer #1: Haircut & Blow-dry, `, "AED 75.00 was refunded to the card automatically"} {
		if !logged(want) {
			t.Errorf("customer cancellation: missing in log: %s", want)
		}
	}
	if r := as(platform.TestRequestInput{Path: "/bookings/1?lang=ar"}); !strings.Contains(r.Body, "المبلغ المسترد") {
		t.Error("Arabic refund line")
	}

	// The owner cancels a paid booking in the admin panel: on_admin_change refunds it and emails the customer.
	book("1", "1", "12:00", "card")
	r := admin(2, map[string]any{"status": "cancelled"})
	if r.Body != "refunded AED 75.00; cancelled email sent" || r.Admin.Row["status"] != "cancelled" || r.Admin.Row["refunded_amount"] != 75.0 || r.Admin.Row["paid_amount"] != 0.0 {
		t.Fatalf("owner cancel: %q %v\n%s", r.Body, r.Admin.Row, logText(r))
	}
	if !logged("a script refunded AED 75.00 of the payment for booking-2") || !logged(`to fatima@mail.ae, "Booking #2 at Lamsa Beauty Lounge is cancelled"`) {
		t.Error("owner cancel: refund or email missing in log")
	}
	if o := as(platform.TestRequestInput{Path: "/bookings/2"}); !strings.Contains(o.Body, "<strong>Cancelled</strong>") || !strings.Contains(o.Body, "<span>Refunded</span><span>AED 75.00</span>") {
		t.Errorf("owner-cancelled booking page: %s", o.Body)
	}

	// The owner moves a pay-at-venue booking: the end follows the start, the customer is emailed the new time.
	book("3", "2", "11:00", "venue") // #3 Classic Manicure with Maryam
	r = admin(3, map[string]any{"time": "15:00"})
	if r.Body != "changed email sent" || r.Admin.Row["end_time"] != "15:30" {
		t.Fatalf("owner time change: %q %v\n%s", r.Body, r.Admin.Row, logText(r))
	}
	if !logged(`to fatima@mail.ae, "Booking #3 changed: `+weekdayOf(tomorrow)+" "+tomorrow+` at 15:00"`) || !free("3", "2", "11:00") || free("3", "2", "15:00") {
		t.Error("owner time change: email or free times")
	}
	// A new staff member: the name follows; moving onto a booked time tells the owner.
	book("3", "3", "16:00", "venue") // #4 with Priya
	r = admin(3, map[string]any{"staff_id": 3, "time": "16:00"})
	if !strings.HasPrefix(r.Body, "double booking: overlaps booking #4") || r.Admin.Row["staff_name"] != "Priya" || !logged(`to the owner, "DOUBLE BOOKING after an admin change #3: `) {
		t.Fatalf("double booking after an admin change: %q %v", r.Body, r.Admin.Row)
	}
	r = admin(4, map[string]any{"status": "no_show"})
	if r.Body != "no_show email sent" || !logged(`to fatima@mail.ae, "We missed you: booking #4"`) {
		t.Fatalf("no show: %q", r.Body)
	}
	r = admin(3, map[string]any{"status": "completed"})
	if r.Body != "completed email sent" || !logged(`to fatima@mail.ae, "Thank you for visiting Lamsa Beauty Lounge"`) {
		t.Fatalf("completed: %q", r.Body)
	}
	if r := admin(3, map[string]any{"phone": "+971509999999"}); r.Body != "nothing to do" {
		t.Fatalf("an unrelated change: %q", r.Body)
	}

	// A monthly class: the card is charged AED 400 every month. Mona enrols (emails per address are limited per day).
	user = "mona@mail.ae"
	if c := req(platform.TestRequestInput{Path: "/c/1"}); !strings.Contains(c.Body, "AED 400.00 per month") || !strings.Contains(c.Body, "6 seats left") {
		t.Fatalf("monthly class page: %s", c.Body)
	}
	if c := as(platform.TestRequestInput{Path: "/c/1?lang=ar"}); !strings.Contains(c.Body, "ادفع شهرياً بالبطاقة: 400.00 د.إ شهرياً") {
		t.Errorf("Arabic monthly payment option: %s", c.Body)
	}
	enrol := as(platform.TestRequestInput{Method: "POST", Path: "/enrol", Form: map[string]string{"class": "1", "participant": "Amal", "name": "Mona", "phone": "+971501234567", "payment": "card"}})
	if !strings.HasPrefix(enrol.Location, "/_rx/pay/rxs_") || !strings.Contains(req(platform.TestRequestInput{Path: enrol.Location}).Body, "400.00 AED every month") {
		t.Fatalf("monthly enrolment: %+v", enrol)
	}
	sub := enrol.Location
	decide := func(decision string) {
		t.Helper()
		if r := req(platform.TestRequestInput{Method: "POST", Path: sub, Form: map[string]string{"decision": decision}}); r.Status != 303 {
			t.Fatalf("%s: %+v", decision, r)
		}
	}
	decide("pay")
	o := as(platform.TestRequestInput{Path: "/bookings/5"})
	for _, want := range []string{"<strong>Confirmed</strong>", "AED 400.00 per month", "Next payment on ", "<span>Paid online</span><span>AED 400.00</span>", "Stop monthly payments"} {
		if !strings.Contains(o.Body, want) {
			t.Errorf("monthly enrolment page lacks %q", want)
		}
	}
	if strings.Contains(o.Body, "Cancel booking") || !strings.Contains(req(platform.TestRequestInput{Path: "/c/1"}).Body, "5 seats left") {
		t.Error("a running monthly enrolment is cancelled like a booking, or holds no seat")
	}
	decide("renew")
	if o := as(platform.TestRequestInput{Path: "/bookings/5"}); !strings.Contains(o.Body, "<span>Paid online</span><span>AED 800.00</span>") {
		t.Errorf("after a renewal: %s", o.Body)
	}
	decide("renewal_failed")
	if o := as(platform.TestRequestInput{Path: "/bookings/5"}); !strings.Contains(o.Body, "Payment overdue") || !strings.Contains(o.Body, "did not go through") ||
		!strings.Contains(o.Body, "<strong>Confirmed</strong>") || !logged(`to mona@mail.ae, "Monthly payment for Everyday Makeup Workshop did not go through"`) {
		t.Errorf("after a failed renewal: %s", o.Body)
	}
	decide("renew")
	if o := as(platform.TestRequestInput{Path: "/bookings/5"}); !strings.Contains(o.Body, "Next payment on ") || !strings.Contains(o.Body, "AED 1200.00") {
		t.Errorf("after a retried renewal: %s", o.Body)
	}
	// The customer stops it: it ends at the end of the month paid, then the seat is free.
	if r := as(platform.TestRequestInput{Method: "POST", Path: "/bookings/5/stop"}); r.Location != "/bookings/5" {
		t.Fatalf("stop: %+v", r)
	}
	if o := as(platform.TestRequestInput{Path: "/bookings/5"}); !strings.Contains(o.Body, "Ends on ") || strings.Contains(o.Body, "Stop monthly payments") ||
		!strings.Contains(req(platform.TestRequestInput{Path: "/c/1"}).Body, "5 seats left") || !logged(`to the owner, "Monthly enrolment stopped by customer #5: `) {
		t.Errorf("after stopping: %s", o.Body)
	}
	if r := as(platform.TestRequestInput{Method: "POST", Path: "/bookings/5/stop"}); r.Location != "/bookings/5?error=error_stop" {
		t.Fatalf("stopping twice: %+v", r)
	}
	decide("renew") // the period ends
	if o := as(platform.TestRequestInput{Path: "/bookings/5"}); !strings.Contains(o.Body, "<strong>Completed</strong>") || !strings.Contains(o.Body, "Ended") ||
		!strings.Contains(req(platform.TestRequestInput{Path: "/c/1"}).Body, "6 seats left") || !logged(`to mona@mail.ae, "Monthly enrolment ended: Everyday Makeup Workshop"`) {
		t.Errorf("after the subscription ended: %s", o.Body)
	}
	if rec := as(platform.TestRequestInput{Path: "/bookings/5/receipt"}); rec.Status != 200 || !strings.Contains(rec.Body, "AED 1200.00") {
		t.Errorf("monthly receipt: %d %s", rec.Status, rec.Body)
	}

	// The owner cancels a running monthly enrolment: the subscription stops now and this month is refunded.
	enrol = as(platform.TestRequestInput{Method: "POST", Path: "/enrol", Form: map[string]string{"class": "1", "participant": "Huda", "name": "Mona", "phone": "+971501234567", "payment": "card"}})
	sub = enrol.Location
	decide("pay")
	decide("renew")
	r = admin(6, map[string]any{"status": "cancelled"})
	if r.Body != "refunded AED 400.00; cancelled email sent" || r.Admin.Row["sub_status"] != "ended" || r.Admin.Row["paid_amount"] != 400.0 || r.Admin.Row["refunded_amount"] != 400.0 {
		t.Fatalf("owner cancels a monthly enrolment: %q %v\n%s", r.Body, r.Admin.Row, logText(r))
	}
	if r := req(platform.TestRequestInput{Method: "POST", Path: sub, Form: map[string]string{"decision": "renew"}}); r.Status != 409 {
		t.Fatalf("a stopped subscription renewed: %+v", r)
	}
	if o := as(platform.TestRequestInput{Path: "/bookings/6"}); !strings.Contains(o.Body, "<strong>Cancelled</strong>") || !strings.Contains(o.Body, "<span>Refunded</span><span>AED 400.00</span>") {
		t.Errorf("owner-cancelled monthly enrolment: %s", o.Body)
	}

	// Paid after the customer cancelled it: the payment is refunded at once and the owner is told.
	pay := as(platform.TestRequestInput{Method: "POST", Path: "/book", Form: map[string]string{"service": "5", "staff": "", "date": tomorrow, "time": "18:00",
		"name": "Mona", "phone": "+971501234568", "payment": "card"}})
	if !strings.HasPrefix(pay.Location, "/_rx/pay/") {
		t.Fatalf("card booking: %+v", pay)
	}
	if r := as(platform.TestRequestInput{Method: "POST", Path: "/bookings/7/cancel"}); r.Location != "/bookings/7" {
		t.Fatalf("cancel before paying: %+v", r)
	}
	req(platform.TestRequestInput{Method: "POST", Path: pay.Location, Form: map[string]string{"decision": "pay"}})
	if o := as(platform.TestRequestInput{Path: "/bookings/7"}); !strings.Contains(o.Body, "<strong>Cancelled</strong>") || !strings.Contains(o.Body, "<span>Refunded</span><span>AED 125.00</span>") ||
		!logged("AED 125.00 was paid by card after the booking was cancelled and has been refunded automatically") {
		t.Errorf("paid after cancelling: %s", o.Body)
	}
}

// weekdayOf is the English weekday name of a YYYY-MM-DD date.
func weekdayOf(date string) string {
	d, _ := time.Parse("2006-01-02", date)
	return d.Weekday().String()
}
