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
	if r := req(platform.TestRequestInput{Path: "/s/1"}); r.Status != 200 || !strings.Contains(r.Body, "Includes VAT") || !strings.Contains(r.Body, `<option value="1">Layla</option>`) ||
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
	if c := req(platform.TestRequestInput{Path: "/c/2"}); !strings.Contains(c.Body, "1 seats left") || !strings.Contains(c.Body, "Ages 8 to 12") || !strings.Contains(c.Body, "Every Sunday") {
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
	if r := other(platform.TestRequestInput{Path: "/bookings"}); strings.Contains(r.Body, "Booking #1 ") || strings.Contains(r.Body, "Haircut") || !strings.Contains(r.Body, "Hind") {
		t.Fatal("someone else's booking listed")
	}
	if r := as(platform.TestRequestInput{Path: "/bookings"}); !strings.Contains(r.Body, "Booking #1 ") || !strings.Contains(r.Body, "Booking #2 ") || strings.Contains(r.Body, "Hind") {
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
