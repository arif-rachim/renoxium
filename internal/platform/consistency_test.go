package platform_test

import (
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/arif-rachim/renoxium/internal/appdef"
)

// What a visitor is shown before they commit must be what they are
// charged. Each finished example is checked end to end: the price the page
// shows (often worked out by the page's own script) against the price the
// server records and asks to be paid.

var zoneOptionRE = regexp.MustCompile(`<option value="(\d+)" data-surcharge="([^"]*)"`)
var dateSelectRE = regexp.MustCompile(`(?s)<select id="date" name="date"[^>]*>(.*?)</select>`)
var optionValueRE = regexp.MustCompile(`<option value="([^"]+)"`)

func TestHomeServiceShownPriceIsCharged(t *testing.T) {
	e := newExampleEnv(t, "home-service-uae", nil)
	const who = "fatima@mail.ae"
	e.signup(who)
	cases := []struct {
		service, qty, zone int
		materials          bool
		want               float64
	}{
		{1, 3, 4, true, 3*40 + 3*10 + 15}, // per hour, materials per hour, Downtown + 15
		{2, 1, 5, false, 450 + 25},        // fixed price, Arabian Ranches + 25
		{3, 4, 1, false, 4 * 35},          // per unit
		{5, 1, 2, false, 110},             // per hour, at the minimum order
	}
	for i, c := range cases {
		t.Run(fmt.Sprintf("service%d", c.service), func(t *testing.T) {
			page := e.get(fmt.Sprintf("/s/%d", c.service), who).Body
			// The page's own price summary.
			opts := zoneOptionRE.FindAllStringSubmatch(page, -1)
			setup := `var z = __el('zone'); z.options = [`
			sel := -1
			for j, o := range opts {
				setup += fmt.Sprintf(`{value: %q, dataset: {surcharge: %q}},`, o[1], o[2])
				if o[1] == fmt.Sprint(c.zone) {
					sel = j
				}
			}
			if sel < 0 {
				t.Fatalf("no zone %d on the page", c.zone)
			}
			setup += fmt.Sprintf(`]; z.selectedIndex = %d; z.value = %q; __el('qty').value = '%d'; __el('materials').checked = %v; __el('date').value = '';`,
				sel, fmt.Sprint(c.zone), c.qty, c.materials)
			vm := pageScript(t, e.p, e.app, who, page, "p-total", setup)
			if got := text(t, vm, "p-total"); got != aed(c.want) {
				t.Errorf("service page shows %s, want %s", got, aed(c.want))
			}

			// A free arrival window, as the page offers them.
			date, slot := "", ""
			for _, d := range optionValueRE.FindAllStringSubmatch(dateSelectRE.FindStringSubmatch(page)[1], -1) {
				var out struct {
					Slots []struct {
						Start string
						Left  int
					}
				}
				json.Unmarshal([]byte(e.get("/api/slots?date="+d[1], who).Body), &out)
				for _, s := range out.Slots {
					if s.Left > 0 && date == "" {
						date, slot = d[1], s.Start
					}
				}
			}
			if date == "" {
				t.Fatal("no free arrival window")
			}
			q := url.Values{"service": {fmt.Sprint(c.service)}, "qty": {fmt.Sprint(c.qty)}, "zone": {fmt.Sprint(c.zone)}, "date": {date}, "slot": {slot}}
			if c.materials {
				q.Set("materials", "1")
			}
			// The checkout page, worked out on the server.
			co := e.get("/checkout?"+q.Encode(), who)
			if co.Status != 200 || !strings.Contains(visibleText(co.Body), aed(c.want)) {
				t.Fatalf("checkout page %d does not show %s", co.Status, aed(c.want))
			}
			// The booking, paid in cash: what is recorded.
			form := map[string]string{"name": "Fatima", "phone": "+971501234567", "address": "Villa 3, Al Barsha", "payment": "cash"}
			for k := range q {
				form[k] = q.Get(k)
			}
			r := e.post("/book", who, form)
			if r.Status != 303 || !strings.HasPrefix(r.Location, "/bookings/") {
				t.Fatalf("book: %+v", r)
			}
			b := e.row(`SELECT total, subtotal, surcharge FROM bookings ORDER BY id DESC LIMIT 1`)
			if num(b["total"]) != c.want {
				t.Errorf("booking %d recorded total %v, want %v", i+1, b["total"], c.want)
			}
		})
	}
}

var whenRE = regexp.MustCompile(`name="when" value="([^"]+)"`)

func TestReservationShownPriceIsCharged(t *testing.T) {
	e := newExampleEnv(t, "reservation-uae", nil)
	const who = "fatima@mail.ae"
	e.signup(who)
	start := dubaiDate(3)
	cases := []struct {
		name      string
		item      int
		departure string // the departure (its id) to choose; weekend departures may have their own price
		query     url.Values
		total     float64 // 0: only checked for consistency
	}{
		{"safari", 1, "1", url.Values{"adults": {"2"}, "children": {"1"}}, 2*150 + 100},
		{"safari with extras", 1, "2", url.Values{"adults": {"2"}, "children": {"0"}, "x1": {"1"}, "x2": {"1"}}, 0},
		{"yacht", 3, "4", url.Values{"adults": {"4"}, "children": {"2"}}, 4*250 + 2*150},
		{"car with child seat", 4, "", url.Values{"start": {start}, "end": {dubaiDate(6)}, "qty": {"1"}, "x3": {"1"}}, 0},
		{"chairs", 6, "", url.Values{"start": {start}, "end": {start}, "qty": {"50"}}, 0},
		{"tent delivered", 7, "", url.Values{"start": {start}, "end": {dubaiDate(4)}, "qty": {"1"}, "x5": {"1"}}, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			q := url.Values{"item": {fmt.Sprint(c.item)}}
			for k, v := range c.query {
				q[k] = v
			}
			if _, ok := q["start"]; !ok {
				w := whenRE.FindAllStringSubmatch(e.get(fmt.Sprintf("/item/%d", c.item), who).Body, -1)
				for _, x := range w {
					if strings.HasPrefix(x[1], c.departure+"_") && q.Get("when") == "" {
						q.Set("when", x[1])
					}
				}
				if q.Get("when") == "" {
					t.Fatalf("no departure %s on the item page", c.departure)
				}
			}
			// The live summary on the item page.
			var quote struct {
				Available bool
				Total     float64
				PayNow    float64 `json:"pay_now"`
				Message   string
				Rows      []struct{ Label, Value, Cls string }
			}
			if err := json.Unmarshal([]byte(e.get("/api/availability?"+q.Encode(), who).Body), &quote); err != nil || !quote.Available {
				t.Fatalf("availability: %v %+v", err, quote)
			}
			if c.total > 0 && quote.Total != c.total {
				t.Errorf("item page total %v, want %v", quote.Total, c.total)
			}
			var shown string
			for _, r := range quote.Rows {
				if r.Cls == "total" {
					shown = r.Value
				}
			}
			if shown != aed(quote.Total) {
				t.Errorf("item page summary shows %q for a total of %v", shown, quote.Total)
			}
			// The checkout page.
			if co := e.get("/checkout?"+q.Encode(), who); !strings.Contains(visibleText(co.Body), aed(quote.Total)) {
				t.Errorf("checkout page does not show %s", aed(quote.Total))
			}
			// The reservation, paying the online share by card.
			form := map[string]string{"name": "Fatima", "phone": "+971501234567", "payment": "card"}
			for k := range q {
				form[k] = q.Get(k)
			}
			r := e.post("/book", who, form)
			if r.Status != 303 || !strings.HasPrefix(r.Location, "/_rx/pay/") {
				t.Fatalf("book: %+v", r)
			}
			row := e.row(`SELECT total, due_online FROM reservations ORDER BY id DESC LIMIT 1`)
			if num(row["total"]) != quote.Total || num(row["due_online"]) != quote.PayNow {
				t.Errorf("reservation recorded total %v, online %v; the item page showed %v, %v", row["total"], row["due_online"], quote.Total, quote.PayNow)
			}
			if got := e.payAmount(r.Location, who); got != quote.PayNow {
				t.Errorf("card checkout asks %v, the item page said %v now", got, quote.PayNow)
			}
		})
	}
}

// dubaiDate is the UAE date days from today, as YYYY-MM-DD.
func dubaiDate(days int) string {
	return time.Now().UTC().Add(4*time.Hour).AddDate(0, 0, days).Format(time.DateOnly)
}

func TestBookingShownDepositIsCharged(t *testing.T) {
	cases := []struct {
		name    string
		percent any // business.deposit_percent
		card    int // business.card_enabled
		want    float64
	}{
		{"half", 50, 1, 75},
		{"thirty", 30, 1, 45},
		{"full", 100, 1, 150},
		{"none", 0, 1, 0},
		{"card off", 50, 0, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := newExampleEnv(t, "booking-uae", func(def *appdef.App) {
				def.Seed["business"][0]["deposit_percent"] = c.percent
				def.Seed["business"][0]["card_enabled"] = c.card
			})
			const who = "fatima@mail.ae"
			e.signup(who)
			page := visibleText(e.get("/s/1", who).Body)                   // Haircut & Blow-dry, AED 150
			offered := strings.Contains(page, "by card now: "+aed(c.want)) // "Pay a deposit by card now: …" or "Pay by card now: …"
			if c.want > 0 && !offered {
				t.Errorf("service page does not offer to pay %s by card", aed(c.want))
			}
			if c.want == 0 && strings.Contains(page, "by card now") {
				t.Error("service page offers a card payment")
			}
			date, slot := "", ""
			for d := 1; d < 14 && date == ""; d++ {
				var out struct{ Slots []struct{ Time string } }
				json.Unmarshal([]byte(e.get("/api/slots?service=1&date="+dubaiDate(d), who).Body), &out)
				if len(out.Slots) > 0 {
					date, slot = dubaiDate(d), out.Slots[0].Time
				}
			}
			if date == "" {
				t.Fatal("no free time")
			}
			r := e.post("/book", who, map[string]string{"service": "1", "staff": "", "date": date, "time": slot, "name": "Fatima", "phone": "+971501234567", "payment": "card"})
			b := e.row(`SELECT price, deposit_due, payment_method FROM bookings ORDER BY id DESC LIMIT 1`)
			if num(b["price"]) != 150 || num(b["deposit_due"]) != c.want {
				t.Errorf("booking recorded price %v, deposit %v; want 150, %v", b["price"], b["deposit_due"], c.want)
			}
			if c.want > 0 {
				if !strings.HasPrefix(r.Location, "/_rx/pay/") {
					t.Fatalf("book: %+v", r)
				}
				if got := e.payAmount(r.Location, who); got != c.want {
					t.Errorf("card checkout asks %v, the page said %v", got, c.want)
				}
			} else if r.Status != 303 || !strings.HasPrefix(r.Location, "/bookings/") {
				t.Fatalf("book without a card payment: %+v", r)
			}
		})
	}
}

var dueNowRE = regexp.MustCompile(`Due now: ([^,]+), AED ([0-9.]+)`)

func TestQuotePortalShownDueIsCharged(t *testing.T) {
	// 12000 + 2 x 1500 = 15000 + 5% VAT = 15750.
	const lines = `[{"name": "Company setup service fee", "qty": 1, "price": 12000}, {"name": "PRO support", "qty": 2, "price": 1500}]`
	cases := []struct {
		name     string
		business any // business.deposit_percent
		quote    any // quotes.deposit_percent: nil uses the business's
		stage    string
		want     float64
	}{
		{"business half", 50, nil, "Deposit", 7875},
		{"quote forty", 50, 40, "Deposit", 6300},
		{"business full", 100, nil, "Deposit", 15750}, // 100% = the full payment up front
		{"quote full", 50, 100, "Deposit", 15750},
		{"no deposit", 0, nil, "", 0}, // nothing due until the owner asks for the balance
		{"quote no deposit", 50, 0, "", 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := newExampleEnv(t, "quote-portal-uae", func(def *appdef.App) {
				def.Seed["business"][0]["deposit_percent"] = c.business
			})
			const who = "fatima@mail.ae"
			e.signup(who)
			if r := e.post("/enquiry", who, map[string]string{"package_id": "1", "name": "Fatima", "phone": "+971501234567", "message": "Two partners."}); r.Status != 303 {
				t.Fatalf("enquiry: %+v", r)
			}
			e.exec(`INSERT INTO quotes (enquiry_id, title, lines, status, deposit_percent) VALUES (1, 'Mainland LLC', ?, 'sent', ?)`, lines, c.quote)
			if r := e.post("/quotes/1/accept", who, map[string]string{"name": "Fatima Al Marri", "agree": "1"}); r.Location != "/quotes/1?done=done_accepted" {
				t.Fatalf("accept: %+v", r)
			}
			page := visibleText(e.get("/quotes/1", who).Body)
			m := dueNowRE.FindStringSubmatch(page)
			if c.want == 0 {
				// No deposit: nothing to pay until the owner asks for the balance.
				if m != nil || !strings.Contains(page, "We will ask for the balance") {
					t.Errorf("quote without a deposit shows %v", m)
				}
				if r := e.post("/quotes/1/pay", who, map[string]string{"method": "card"}); r.Location != "/quotes/1?error=error_not_due" {
					t.Errorf("paying a quote without a deposit: %+v", r)
				}
				return
			}
			if m == nil {
				t.Fatal("the quote page shows nothing due")
			}
			if m[2] != fmt.Sprintf("%.2f", c.want) {
				t.Errorf("quote page shows due now %s AED %s, want %.2f", m[1], m[2], c.want)
			}
			if c.stage != "" && m[1] != c.stage {
				t.Errorf("due now is %q, want %q", m[1], c.stage)
			}
			r := e.post("/quotes/1/pay", who, map[string]string{"method": "card"})
			if !strings.HasPrefix(r.Location, "/_rx/pay/") {
				t.Fatalf("pay by card: %+v", r)
			}
			if got := e.payAmount(r.Location, who); got != c.want {
				t.Errorf("card checkout asks %v, the quote page said %v", got, c.want)
			}
		})
	}
}

func TestStoreShownTotalIsCharged(t *testing.T) {
	e := newExampleEnv(t, "store-uae", nil) // AED 15 delivery, free from AED 200
	const who = "fatima@mail.ae"
	e.signup(who)
	for _, cart := range []string{
		`[{"id":2,"qty":1}]`,                        // 95 + 15
		`[{"id":1,"v":2,"qty":1},{"id":2,"qty":2}]`, // 420 + 190, free delivery
		`[{"id":3,"qty":1},{"id":2,"qty":0}]`,       // a quantity under 1 counts as 1
		`[{"id":4,"qty":1},{"id":1,"v":1,"qty":2},{"id":3,"qty":3}]`,
	} {
		page := e.get("/cart", who).Body
		vm := pageScript(t, e.p, e.app, who, page, "cart-field", `var __cart = `+cart+`; function rxCart() { return __cart; } function rxSave(c) { __cart = c; }`)
		shown := text(t, vm, "total")
		saved, _ := vm.RunString(`JSON.stringify(__cart)`) // what the page puts in the order form
		r := e.post("/checkout", who, map[string]string{"cart": saved.String(), "name": "Fatima", "phone": "+971501234567", "address": "Villa 3", "emirate": "Dubai", "payment": "cod"})
		if r.Status != 303 || !strings.HasPrefix(r.Location, "/orders/") {
			t.Fatalf("checkout %s: %+v", cart, r)
		}
		o := e.row(`SELECT total FROM orders ORDER BY id DESC LIMIT 1`)
		if shown != aed(num(o["total"])) {
			t.Errorf("cart %s: the page shows %s, the order is %v", cart, shown, o["total"])
		}
	}
}
