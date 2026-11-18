package platform_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/arif-rachim/renoxium/examples"
	"github.com/arif-rachim/renoxium/internal/appdef"
	"github.com/arif-rachim/renoxium/internal/platform"
)

// dubaiDay is the UAE local date n days from today, as the example stores it.
func dubaiDay(n int) string {
	return time.Now().UTC().Add(4*time.Hour).AddDate(0, 0, n).Format("2006-01-02")
}

func reservationApp(t *testing.T) *platform.Platform {
	t.Helper()
	p := newPlatform(t)
	ex, ok := examples.Get("reservation-uae")
	if !ok {
		t.Fatal("reservation-uae is not registered")
	}
	def, problems := appdef.Parse(ex.JSON)
	if len(problems) > 0 {
		t.Fatal(problems)
	}
	p.CreateApp("usr_test", "sahara")
	if res, err := p.ApplyChange(context.Background(), "sahara", def); err != nil || !res.OK {
		t.Fatal(err, res)
	}
	return p
}

// The UAE reservation example works end to end on preview: tours booked by
// seats on a departure, rentals booked by date range, a card deposit
// (simulated), VAT, availability that follows the reservations, and
// reservations private to their customer.
func TestReservationUAEExample(t *testing.T) {
	p := reservationApp(t)
	ctx := context.Background()
	req := func(in platform.TestRequestInput) platform.TestResponse {
		t.Helper()
		r, err := p.TestRequest(ctx, "sahara", in)
		if err != nil {
			t.Fatalf("%+v: %v", in, err)
		}
		if r.Status >= 500 {
			t.Fatalf("%s %s: %d\n%s", in.Method, in.Path, r.Status, logText(r))
		}
		return r
	}
	type availability struct {
		Complete  bool
		Available bool
		Left      float64
		Total     float64
		Message   string
	}
	avail := func(query string) availability {
		t.Helper()
		r := req(platform.TestRequestInput{Path: "/api/availability?" + query})
		var a availability
		if err := json.Unmarshal([]byte(r.Body), &a); err != nil {
			t.Fatalf("availability %s: %s", query, r.Body)
		}
		return a
	}
	// Item 3 is the yacht trip; its departure 4 runs daily at 17:00 with 12 seats.
	// Item 4 is the Nissan Sunny (3 cars, AED 90 a day, AED 560 a week); item 8 the bouncy castle (1).
	yacht := func(day, adults int) string { return fmt.Sprintf("item=3&when=4_%s&adults=%d", dubaiDay(day), adults) }
	rental := func(item, from, to, qty int) string {
		return fmt.Sprintf("item=%d&start=%s&end=%s&qty=%d", item, dubaiDay(from), dubaiDay(to), qty)
	}

	home := req(platform.TestRequestInput{Path: "/"})
	for _, want := range []string{`dir="ltr"`, "Evening Desert Safari", "From AED 150.00", "Nissan Patrol", "From AED 450.00", "Party rental", "DET tourism licence 1234567"} {
		if !strings.Contains(home.Body, want) {
			t.Errorf("home lacks %q", want)
		}
	}
	ar := req(platform.TestRequestInput{Path: "/?lang=ar"})
	if !strings.Contains(ar.Body, `dir="rtl"`) || !strings.Contains(ar.Body, "سفاري صحراوي مسائي") || !strings.Contains(ar.Body, "سهارى باي") {
		t.Fatal("arabic home")
	}
	if r := req(platform.TestRequestInput{Path: "/?c=Cars"}); strings.Contains(r.Body, "Dhow Dinner") || !strings.Contains(r.Body, "Nissan Sunny") {
		t.Fatal("category filter")
	}
	if r := req(platform.TestRequestInput{Path: "/item/3"}); r.Status != 200 || !strings.Contains(r.Body, `value="4_`+dubaiDay(5)+`"`) ||
		!strings.Contains(r.Body, "12 seats left") || !strings.Contains(r.Body, "Hotel pickup") {
		t.Fatalf("tour page: %d %s", r.Status, r.Body)
	}
	if r := req(platform.TestRequestInput{Path: "/item/4?lang=ar"}); !strings.Contains(r.Body, `name="start"`) || !strings.Contains(r.Body, "مقعد أطفال") ||
		!strings.Contains(r.Body, "AED 1000.00") {
		t.Fatalf("rental page: %s", r.Body)
	}
	if r := req(platform.TestRequestInput{Path: "/item/999"}); r.Status != 404 {
		t.Fatalf("missing item: %d", r.Status)
	}
	if a := avail(yacht(5, 1)); !a.Available || a.Left != 12 || a.Total != 250 {
		t.Fatalf("fresh departure: %+v", a)
	}
	if a := avail("item=3&adults=2"); a.Complete || a.Available {
		t.Fatalf("no departure chosen: %+v", a)
	}

	// 2 adults at 250 + 1 child at 150 + hotel pickup at 30 a person = 740; 20% is paid online; VAT included = 740 * 5/105.
	seats := map[string]string{"item": "3", "when": "4_" + dubaiDay(5), "adults": "2", "children": "1", "x1": "1", "name": "Amna", "phone": "+971501234567",
		"notes": "Pickup from Rove Downtown", "payment": "card"}
	if r := req(platform.TestRequestInput{Method: "POST", Path: "/book", Form: seats}); r.Status != 303 || !strings.HasPrefix(r.Location, "/_rx/auth/login?next=") {
		t.Fatalf("signed out booking: %+v", r)
	}
	req(platform.TestRequestInput{Method: "POST", Path: "/_rx/auth/signup", Form: map[string]string{"name": "Amna", "email": "amna@mail.ae", "password": "amna password"}})
	as := func(in platform.TestRequestInput) platform.TestResponse {
		t.Helper()
		in.UserEmail = "amna@mail.ae"
		return req(in)
	}
	book := func(form map[string]string) platform.TestResponse {
		t.Helper()
		return as(platform.TestRequestInput{Method: "POST", Path: "/book", Form: form})
	}
	checkoutPath := "/checkout?item=3&when=4_" + dubaiDay(5) + "&adults=2&children=1&x1=1"
	if r := req(platform.TestRequestInput{Path: checkoutPath}); !strings.Contains(r.Body, "Sign in to reserve") || strings.Contains(r.Body, `action="/book"`) {
		t.Fatal("checkout for a signed-out visitor")
	}
	co := as(platform.TestRequestInput{Path: checkoutPath})
	for _, want := range []string{`action="/book"`, `value="Amna"`, "AED 740.00", "Pay by card now: AED 148.00", "AED 592.00", "Reserve now, pay on the day"} {
		if !strings.Contains(co.Body, want) {
			t.Errorf("checkout lacks %q", want)
		}
	}
	r := book(seats)
	if r.Status != 303 || !strings.HasPrefix(r.Location, "/_rx/pay/") {
		t.Fatalf("card booking: %+v", r)
	}
	if !strings.Contains(req(platform.TestRequestInput{Path: r.Location}).Body, "148.00 AED") {
		t.Fatal("deposit charged")
	}
	if a := avail(yacht(5, 1)); a.Left != 9 {
		t.Fatalf("seats held while paying: %+v", a)
	}
	paid := req(platform.TestRequestInput{Method: "POST", Path: r.Location, Form: map[string]string{"decision": "pay"}})
	if paid.Location != "/reservations/1?placed=1" {
		t.Fatalf("after paying: %+v", paid)
	}
	page := as(platform.TestRequestInput{Path: "/reservations/1?placed=1"})
	for _, want := range []string{"<strong>Confirmed</strong>", "Thank you", "Sunset Yacht Trip", "AED 740.00", "AED 35.24", "AED 148.00", "AED 592.00", "Rove Downtown", "Cancel reservation"} {
		if !strings.Contains(page.Body, want) {
			t.Errorf("reservation page lacks %q", want)
		}
	}
	if a := avail(yacht(5, 1)); a.Left != 9 {
		t.Fatalf("seats after paying: %+v", a)
	}
	if a := avail(yacht(6, 1)); a.Left != 12 {
		t.Fatalf("another day is untouched: %+v", a)
	}

	// Overbooking is refused: 10 seats when 9 are left.
	over := map[string]string{"item": "3", "when": "4_" + dubaiDay(5), "adults": "10", "name": "Amna", "phone": "+971501234567", "payment": "later"}
	if r := book(over); r.Location != "/item/3?error=error_seats&n=9" {
		t.Fatalf("overbooking: %+v", r)
	}
	if r := req(platform.TestRequestInput{Path: "/item/3?error=error_seats&n=9"}); !strings.Contains(r.Body, "Not enough seats on this departure (9 left).") {
		t.Fatal("overbooking message")
	}
	if a := avail(yacht(5, 10)); a.Available || a.Left != 9 {
		t.Fatalf("availability for too many: %+v", a)
	}
	// A departure in the past, or on a day it does not run, is refused.
	over["when"], over["adults"] = "4_"+dubaiDay(-1), "1"
	if r := book(over); r.Location != "/item/3?error=error_departure&n=0" {
		t.Fatalf("past departure: %+v", r)
	}

	// A rental: 2 cars for 4 days at 90 = 720, paid on the day, AED 1000 security deposit a car at handover.
	car := func(from, to, qty int) map[string]string {
		return map[string]string{"item": "4", "start": dubaiDay(from), "end": dubaiDay(to), "qty": fmt.Sprint(qty), "name": "Amna", "phone": "+971501234567",
			"notes": "Licence 1234567", "payment": "later"}
	}
	if r := book(car(3, 7, 2)); r.Location != "/reservations/2?placed=1" {
		t.Fatalf("rental: %+v", r)
	}
	rent := as(platform.TestRequestInput{Path: "/reservations/2"})
	for _, want := range []string{"<strong>Confirmed</strong>", "Nissan Sunny – 4 days × 2", "AED 720.00", "AED 34.29", "AED 0.00", "AED 2000.00", "(4 days)"} {
		if !strings.Contains(rent.Body, want) {
			t.Errorf("rental page lacks %q", want)
		}
	}
	// 3 cars owned: an overlapping range needing 2 more is refused, with 1 left; the return day counts as taken.
	if r := book(car(5, 6, 2)); r.Location != "/item/4?error=error_stock&n=1" {
		t.Fatalf("overlapping rental: %+v", r)
	}
	if r := book(car(7, 9, 2)); r.Location != "/item/4?error=error_stock&n=1" {
		t.Fatalf("rental starting on the return day: %+v", r)
	}
	if a := avail(rental(4, 1, 20, 1)); !a.Available || a.Left != 1 {
		t.Fatalf("long range across a busy one: %+v", a)
	}
	// A range after it succeeds.
	if r := book(car(8, 9, 2)); r.Location != "/reservations/3?placed=1" {
		t.Fatalf("later rental: %+v", r)
	}
	// Prices by the day and the week: 9 days = 560 + 2 x 90; 6 days = 540; a same-day return is one day.
	for _, c := range []struct {
		from, to int
		total    float64
	}{{20, 29, 740}, {20, 26, 540}, {20, 27, 560}, {20, 33, 1100}, {20, 20, 90}} {
		if a := avail(rental(4, c.from, c.to, 1)); !a.Available || a.Total != c.total {
			t.Errorf("%d days: %+v, want %v", c.to-c.from, a, c.total)
		}
	}
	// The tent is 600 a day or 3000 a week: 6 days never cost more than the week.
	if a := avail(rental(7, 20, 26, 1)); !a.Available || a.Total != 3000 {
		t.Errorf("6 days of a tent: %+v", a)
	}
	// The Patrol has a two-day minimum; a return before the start is refused.
	if a := avail(rental(5, 3, 4, 1)); a.Available || a.Message != "The minimum rental is 2 days." {
		t.Errorf("minimum days: %+v", a)
	}
	if a := avail(rental(5, 3, 5, 1)); !a.Available || a.Total != 900 {
		t.Errorf("patrol for two days: %+v", a)
	}
	if a := avail(rental(4, 5, 4, 1)); a.Available {
		t.Errorf("end before start: %+v", a)
	}

	// A failed card payment frees the seats.
	seats["when"], seats["adults"], seats["children"] = "4_"+dubaiDay(6), "5", "0"
	r = book(seats)
	if a := avail(yacht(6, 1)); a.Left != 7 {
		t.Fatalf("seats before failure: %+v", a)
	}
	req(platform.TestRequestInput{Method: "POST", Path: r.Location, Form: map[string]string{"decision": "fail"}})
	if a := avail(yacht(6, 1)); a.Left != 12 {
		t.Fatalf("seats after failure: %+v", a)
	}
	if o := as(platform.TestRequestInput{Path: "/reservations/4"}); !strings.Contains(o.Body, "Payment failed") || !strings.Contains(o.Body, `action="/reservations/4/pay"`) {
		t.Fatal("failed reservation page")
	}
	// The same for stock: the only bouncy castle is held while paying and free again after the failure.
	castle := map[string]string{"item": "8", "start": dubaiDay(3), "end": dubaiDay(3), "qty": "1", "name": "Amna", "phone": "+971501234567", "payment": "card"}
	r = book(castle)
	if !strings.Contains(req(platform.TestRequestInput{Path: r.Location}).Body, "70.00 AED") {
		t.Fatal("castle deposit: 20% of 350")
	}
	if a := avail(rental(8, 3, 4, 1)); a.Available || a.Left != 0 {
		t.Fatalf("castle while paying: %+v", a)
	}
	req(platform.TestRequestInput{Method: "POST", Path: r.Location, Form: map[string]string{"decision": "fail"}})
	if a := avail(rental(8, 3, 4, 1)); !a.Available || a.Left != 1 {
		t.Fatalf("castle after failure: %+v", a)
	}
	// Paying again takes the reservation back to the payment page and confirms it.
	r = as(platform.TestRequestInput{Method: "POST", Path: "/reservations/5/pay"})
	if !strings.HasPrefix(r.Location, "/_rx/pay/") {
		t.Fatalf("pay again: %+v", r)
	}
	req(platform.TestRequestInput{Method: "POST", Path: r.Location, Form: map[string]string{"decision": "pay"}})
	if o := as(platform.TestRequestInput{Path: "/reservations/5"}); !strings.Contains(o.Body, "<strong>Confirmed</strong>") || !strings.Contains(o.Body, "AED 280.00") {
		t.Fatalf("paid again: %s", o.Body)
	}

	// Missing details go back to the item with a message.
	bad := car(30, 31, 1)
	bad["phone"] = ""
	if r := book(bad); r.Location != "/item/4?error=error_fields" {
		t.Fatalf("missing phone: %+v", r)
	}

	// Voucher: 740 including VAT = 704.76 + 35.24, with the licence; not yet a tax invoice without a TRN.
	v := as(platform.TestRequestInput{Path: "/reservations/1/voucher"})
	for _, want := range []string{"<h1>Booking voucher</h1>", "Sahara Bay Tourism &amp; Rentals LLC", "DET tourism licence 1234567", "AED 704.76", "AED 35.24", "AED 740.00",
		"AED 592.00", "Hotel pickup and drop-off × 3", "Amna"} {
		if !strings.Contains(v.Body, want) {
			t.Errorf("voucher lacks %q", want)
		}
	}
	if v := as(platform.TestRequestInput{Path: "/reservations/1/voucher?lang=ar"}); !strings.Contains(v.Body, `dir="rtl"`) || !strings.Contains(v.Body, "قسيمة الحجز") {
		t.Error("arabic voucher")
	}

	// Reservations are private: another visitor sees neither the list entry nor the pages, and cannot cancel.
	req(platform.TestRequestInput{Method: "POST", Path: "/_rx/auth/signup", Form: map[string]string{"email": "other@mail.ae", "password": "other password"}})
	other := func(method, path string) platform.TestResponse {
		return req(platform.TestRequestInput{Method: method, Path: path, UserEmail: "other@mail.ae"})
	}
	for _, path := range []string{"/reservations/1", "/reservations/1/voucher"} {
		if r := other("GET", path); r.Status != 404 || strings.Contains(r.Body, "Rove Downtown") {
			t.Fatalf("someone else's %s: %d", path, r.Status)
		}
	}
	if r := other("POST", "/reservations/1/cancel"); r.Status != 404 {
		t.Fatalf("someone else's cancel: %d", r.Status)
	}
	if r := other("GET", "/reservations"); strings.Contains(r.Body, "Reservation #1<") || !strings.Contains(r.Body, "no reservations yet") {
		t.Fatal("someone else's reservation listed")
	}
	if r := as(platform.TestRequestInput{Path: "/reservations"}); !strings.Contains(r.Body, "Reservation #1<") || !strings.Contains(r.Body, "Reservation #2<") {
		t.Fatal("own reservations list")
	}
	if r := req(platform.TestRequestInput{Path: "/reservations/1"}); r.Status != 303 {
		t.Fatalf("signed out reservation page: %d", r.Status)
	}

	// The customer cancels reservation 3, which starts in 8 days: the 2 cars are free again.
	if a := avail(rental(4, 8, 9, 2)); a.Available {
		t.Fatalf("before cancelling: %+v", a)
	}
	if r := as(platform.TestRequestInput{Method: "POST", Path: "/reservations/3/cancel"}); r.Location != "/reservations/3?cancelled=1" {
		t.Fatalf("cancel: %+v", r)
	}
	if o := as(platform.TestRequestInput{Path: "/reservations/3?cancelled=1"}); !strings.Contains(o.Body, "<strong>Cancelled</strong>") || !strings.Contains(o.Body, "is cancelled") ||
		strings.Contains(o.Body, "Cancel reservation") {
		t.Fatalf("cancelled page: %s", o.Body)
	}
	if a := avail(rental(4, 8, 9, 3)); !a.Available || a.Left != 3 {
		t.Fatalf("after cancelling: %+v", a)
	}
	// Tomorrow's rental starts inside the 24-hour free-cancellation window: it cannot be cancelled online.
	if r := book(car(1, 2, 1)); r.Location != "/reservations/6?placed=1" {
		t.Fatalf("rental for tomorrow: %+v", r)
	}
	if r := as(platform.TestRequestInput{Method: "POST", Path: "/reservations/6/cancel"}); r.Location != "/reservations/6" {
		t.Fatalf("late cancel: %+v", r)
	}
	if o := as(platform.TestRequestInput{Path: "/reservations/6"}); !strings.Contains(o.Body, "<strong>Confirmed</strong>") || !strings.Contains(o.Body, "Online cancellation is closed") {
		t.Fatal("late cancel changed the reservation")
	}

	// Preview never sends email: confirmations and owner notices are in the log.
	logs, _ := p.Logs("sahara", logFilterAll())
	for _, want := range []string{
		`not sent from preview: to amna@mail.ae, "Your reservation #1 at Sahara Bay Tours & Rentals"`,
		"https://sahara--preview.renoxium.test/reservations/2/voucher",
		`to the owner, "New reservation #1: Sunset Yacht Trip, ` + dubaiDay(5) + ` 17:00, 3 pax, AED 740.00 (card, AED 148.00 paid)"`,
		`to the owner, "New reservation #2: Nissan Sunny, ` + dubaiDay(3) + ` to ` + dubaiDay(7) + `, 2 x 4d, AED 720.00 (pay later)"`,
		`to amna@mail.ae, "Reservation #3 cancelled"`,
		`to the owner, "Cancelled by customer #3: Nissan Sunny, ` + dubaiDay(8) + `, nothing paid"`,
	} {
		if countMessages(logs, want) == 0 {
			t.Errorf("missing email in log: %s", want)
		}
	}

	// The daily job: tomorrow's dhow cruise (departure 3, 20:30) with 3 passengers and one car handover go to the owner,
	// and both customers are reminded once.
	dhow := map[string]string{"item": "2", "when": "3_" + dubaiDay(1), "adults": "2", "children": "1", "name": "Amna", "phone": "+971501234567", "payment": "later"}
	if r := book(dhow); r.Location != "/reservations/7?placed=1" {
		t.Fatalf("dhow for tomorrow: %+v", r)
	}
	summary := "tomorrow " + dubaiDay(1) + ": 1 departure (3 passengers), 1 handover, 0 returns"
	if r, err := p.TestRequest(ctx, "sahara", platform.TestRequestInput{Job: "daily"}); err != nil || r.Status != 200 || r.Body != summary+"; 2 reminded; 0 expired" {
		t.Fatalf("daily job: %v %+v\n%s", err, r, logText(r))
	}
	logs, _ = p.Logs("sahara", logFilterAll())
	for _, want := range []string{
		`to the owner, "Tomorrow ` + dubaiDay(1) + `: 1 departure (3 passengers), 1 handover, 0 returns"`,
		`to amna@mail.ae, "Reminder: Marina Dhow Dinner Cruise tomorrow (reservation #7)"`,
		`to amna@mail.ae, "Reminder: Nissan Sunny tomorrow (reservation #6)"`,
	} {
		if countMessages(logs, want) == 0 {
			t.Errorf("missing daily email in log: %s", want)
		}
	}
	if r, _ := p.TestRequest(ctx, "sahara", platform.TestRequestInput{Job: "daily"}); r.Body != summary+"; 0 reminded; 0 expired" {
		t.Fatalf("second daily run reminds again: %+v", r)
	}
}

func TestReservationUAETools(t *testing.T) {
	p := reservationApp(t)
	ctx := context.Background()
	call := func(name string, args map[string]any) string {
		t.Helper()
		r, err := p.TestRequest(ctx, "sahara", platform.TestRequestInput{Method: "POST", Path: "/mcp",
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
	if out := call("search_items", map[string]any{"query": "patrol"}); !strings.Contains(out, `"name":"Nissan Patrol"`) || !strings.Contains(out, `"price_per_day":450`) ||
		!strings.Contains(out, `"url":"https://sahara--preview.renoxium.test/item/5"`) || strings.Contains(out, "Sunny") {
		t.Errorf("search: %s", out)
	}
	if out := call("search_items", map[string]any{"category": "Tours", "lang": "ar"}); !strings.Contains(out, "رحلة يخت عند الغروب") || strings.Contains(out, "نيسان") {
		t.Errorf("search by category: %s", out)
	}
	// The yacht runs daily at 17:00: 2 adults and 1 child cost 650, of which 20% online.
	out := call("check_availability", map[string]any{"item": 3, "date": dubaiDay(4), "people": 2, "children": 1})
	for _, want := range []string{`"available":true`, `"date":"` + dubaiDay(4) + `"`, `"time":"17:00"`, `"seats_left":12`, `"total_price":650`, `"pay_online_now":130`} {
		if !strings.Contains(out, want) {
			t.Errorf("tour availability lacks %s: %s", want, out)
		}
	}
	if out := call("check_availability", map[string]any{"item": 3, "date": dubaiDay(4), "people": 13}); !strings.Contains(out, `"available":false`) {
		t.Errorf("13 people on 12 seats: %s", out)
	}
	out = call("check_availability", map[string]any{"item": 4, "start": dubaiDay(2), "end": dubaiDay(9), "quantity": 3})
	for _, want := range []string{`"available":true`, `"days":7`, `"stock_left":3`, `"total_price":1680`, `"security_deposit_at_handover":3000`} {
		if !strings.Contains(out, want) {
			t.Errorf("rental availability lacks %s: %s", want, out)
		}
	}
	if out := call("check_availability", map[string]any{"item": 4, "start": dubaiDay(2), "end": dubaiDay(3), "quantity": 4}); !strings.Contains(out, `"available":false`) ||
		!strings.Contains(out, `"stock_left":3`) {
		t.Errorf("more cars than owned: %s", out)
	}
	if out := call("business_info", nil); !strings.Contains(out, `"payment_methods":["card online","pay on the day"]`) || !strings.Contains(out, `"paid_online_percent":20`) ||
		!strings.Contains(out, `"free_cancellation_hours_before_start":24`) {
		t.Errorf("business info: %s", out)
	}
	if r, err := p.TestRequest(ctx, "sahara", platform.TestRequestInput{Job: "daily"}); err != nil || r.Status != 200 ||
		r.Body != "tomorrow "+dubaiDay(1)+": 0 departures (0 passengers), 0 handovers, 0 returns; 0 reminded; 0 expired" {
		t.Errorf("daily job without reservations: %v %+v", err, r)
	}
}
