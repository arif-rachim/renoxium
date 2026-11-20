package platform_test

import (
	"strings"
	"testing"

	"github.com/arif-rachim/renoxium/internal/appdef"
)

// The boutique theme of reservation-uae: the banner photo as a backdrop with
// a see-through header, a large rounded card with the headline, a search
// pill that really filters (category, date, guests), numbers counted from
// the tables, facts and a FAQ built from the settings row, the first
// category as tall cards and the others as compact cards, extras,
// highlights, three steps, a closing band and a forest-green footer with a
// stamp when business.font is boutique; the usual layout for every other
// font. Guest reviews are shown only when the owner adds real ones; the seed
// has none.
func TestReservationBoutiqueTheme(t *testing.T) {
	e := newExampleEnv(t, "reservation-uae", nil)
	home := e.get("/", "").Body
	for _, want := range []string{`data-font="boutique"`, "--brand:#b85c3c", `class="r-top"`, "Desert · Sea · City",
		`<h1 id="hero-h">Dunes at sunset, <span class="accent">the Marina by night.</span></h1>`, "Pick a date and reserve online.",
		`class="btn hdr-cta"`, `class="r-out"`, `class="r-card"`, `class="r-panel"`,
		// The search pill: a real GET form of the home page.
		`<form class="r-find" method="get" action="/#items"`, `name="c"`, `<option value="Cars">Cars</option>`, `name="date" type="date" min="` + dubaiDate(0) + `" max="` + dubaiDate(60) + `"`,
		`name="guests"`, `class="r-go"`,
		// Counted from the tables, and facts from the settings row.
		`<a href="#items"><strong>3</strong><span>Tours and cruises</span></a>`, `<strong>5</strong><span>Rentals</span>`, `<strong>3</strong><span>Categories</span>`,
		`<strong>23</strong><span>Departures a week</span>`, `<a href="#extras"><strong>5</strong><span>Extras to add</span></a>`,
		`class="r-facts"`, "20% by card to reserve<small>Balance on the day · Or reserve now, pay on the day</small>", "Free until 24 hours before",
		"Up to 60 days ahead", "Prices include VAT (5%)", "Held on your card, not charged", "Office 804, Bay Square 3, Business Bay, Dubai",
		// Every item is on the page: the first category as tall cards, the others as compact cards.
		`id="r-track"`, `data-scroll="1"`, `class="r-tall" href="/item/1"`, "Tours · From AED 150.00 · 6 hours", `class="r-tall" href="/item/3"`,
		`<a href="#cat-2">Cars<span>2</span></a>`, `<a href="#cat-3">Party rental<span>3</span></a>`, `class="r-group" id="cat-2"`, `<h3>Party rental<span>3 options</span></h3>`,
		`class="r-item" href="/item/4"`, "From AED 90.00 per day", "AED 2800.00 per week · Minimum rental: 2 days", `class="r-item" href="/item/8"`,
		"Add to your trip", "<span>Hotel pickup and drop-off</span><span class=\"price\">&#43; AED\u00a030 <small>per person</small>", "<small>per booking</small>",
		"Why book with us", "Licensed operator", "Live availability", "Voucher by email", "since 2009",
		`id="how"`, "Three steps to your trip", "Choose a date", "Show your voucher",
		`id="faq"`, `<details open><summary>How do I pay?</summary><p>You pay 20% by card when you reserve and the balance on the day. You can also reserve now and pay on the day.</p>`,
		"free of charge on the website until 24 hours before the start", "Is there a security deposit on rentals?", "What do I bring for a rental?",
		`class="r-close-in"`, "Ready when you are",
		`class="site-foot b-foot r-foot"`, `class="stamp"`, "Tours · Rentals · Book online", "Quick links", `<a href="/?c=Cars#items">Cars<span class="r-fn">2</span></a>`,
		"DET tourism licence 1234567"} {
		if !strings.Contains(home, want) {
			t.Errorf("boutique home lacks %q", want)
		}
	}
	// No reviews without real ones, no old home, no arch frame and no photo without one.
	for _, unwanted := range []string{`class="r-reviews"`, "(sample)", `class="intro`, `class="tiles"`, `class="arch`, ` arch"`, "999px 999px", `class="r-stack"`, `r-found`} {
		if strings.Contains(home, unwanted) {
			t.Errorf("boutique home shows %q", unwanted)
		}
	}
	if n := strings.Count(home, `class="r-tall"`); n != 3 {
		t.Errorf("%d tall cards, want 3", n)
	}
	if n := strings.Count(home, `class="r-item"`); n != 5 {
		t.Errorf("%d compact cards, want 5", n)
	}
	ar := e.get("/?lang=ar", "").Body
	for _, want := range []string{`dir="rtl"`, `<h1 id="hero-h">الكثبان عند الغروب، <span class="accent">والمارينا ليلاً.</span></h1>`, "صحراء · بحر · مدينة",
		`name="lang" value="ar"`, "انطلاقات أسبوعياً", "20% بالبطاقة لتأكيد الحجز", `class="r-tall" href="/item/1?lang=ar"`, "كل ما يمكنك حجزه", "أضف إلى رحلتك",
		"لماذا تحجز معنا", "مشغّل مرخّص", "ثلاث خطوات إلى رحلتك", "قبل أن تحجز", "كيف أدفع؟", "جاهزون متى شئت", "روابط سريعة",
		`<a href="/?c=Cars&amp;lang=ar#items">السيارات<span class="r-fn">2</span></a>`} {
		if !strings.Contains(ar, want) {
			t.Errorf("Arabic boutique home lacks %q", want)
		}
	}

	// The search: a category, and what is free on a date for a number of guests (the yacht has 12 seats).
	cars := e.get("/?c=Cars", "").Body
	for _, want := range []string{`class="r-sec r-found"`, `<h2 id="res-h">2 options</h2><p>Cars</p>`, "Clear search", `<option value="Cars" selected>`, `class="r-item" href="/item/5"`} {
		if !strings.Contains(cars, want) {
			t.Errorf("category search lacks %q", want)
		}
	}
	if strings.Contains(cars, `class="r-tall"`) || strings.Contains(cars, "Dhow") || strings.Contains(cars, "Chiavari") {
		t.Error("category search shows other categories")
	}
	day := dubaiDate(3)
	found := e.get("/?c=Tours&date="+day+"&guests=13", "").Body
	for _, want := range []string{`<h2 id="res-h">2 options</h2>`, "13 guests", `value="` + day + `"`, `value="13"`, "6 hours · 15:00 · 40 seats left", "20:30 · 60 seats left"} {
		if !strings.Contains(found, want) {
			t.Errorf("date search lacks %q", want)
		}
	}
	if strings.Contains(found, "Yacht") || strings.Contains(found, "Nissan") {
		t.Error("date search shows a tour without enough seats, or another category")
	}
	if all := e.get("/?date="+day+"&guests=2", "").Body; !strings.Contains(all, `<h2 id="res-h">8 options</h2>`) || !strings.Contains(all, "AED 560.00 per week · 3 available for these dates") ||
		!strings.Contains(all, "12 seats left") {
		t.Error("date search over every category")
	}
	if none := e.get("/?c=Tours&date="+day+"&guests=99", "").Body; !strings.Contains(none, "Nothing to book in this category yet") || strings.Contains(none, `class="r-item"`) {
		t.Error("search without a match")
	}
	// A date outside the bookable period is ignored.
	if past := e.get("/?date=2020-01-01", "").Body; strings.Contains(past, `r-found"`) || !strings.Contains(past, `class="r-tall"`) {
		t.Error("a past date searched")
	}
	// The other pages keep their layout, with the boutique look and footer.
	if s := e.get("/item/999", "").Body; !strings.Contains(s, `class="site-foot b-foot r-foot"`) || !strings.Contains(s, `class="btn hdr-cta"`) || strings.Contains(s, `class="r-top"`) {
		t.Error("another page in boutique")
	}
	if s := e.get("/item/1", "").Body; !strings.Contains(s, `id="reserve"`) || strings.Contains(s, `class="r-top"`) {
		t.Error("item page in boutique")
	}

	// Real guest reviews, added by the owner, are shown (three at most); photos become postcards.
	rated := newExampleEnv(t, "reservation-uae", func(def *appdef.App) {
		var rows []map[string]any
		for i, n := range []string{"sara M.", "Karim B.", "Fatima Z.", "Not shown"} {
			rows = append(rows, map[string]any{"quote": "Quote of " + n, "quote_ar": "رأي", "name": n, "area": "Desert safari", "position": i + 1, "active": 1})
		}
		def.Seed["testimonials"] = rows
		def.Seed["business"][0]["story_image"] = "/camp.jpg"
		def.Seed["business"][0]["hero_image"] = "/dunes.jpg"
	})
	h := rated.get("/", "").Body
	for _, want := range []string{`class="r-sec r-reviews"`, "What our guests say", "Quote of sara M.", "<strong>Karim B.</strong>", "Quote of Fatima Z.", "Desert safari",
		`aria-hidden="true">S</span>`, `class="r-top has-photo"`, `class="r-card-img" src="/dunes.jpg?w=1600"`, `class="r-stack"`, `class="r-post front"><img src="/camp.jpg?w=960"`} {
		if !strings.Contains(h, want) {
			t.Errorf("home with reviews lacks %q", want)
		}
	}
	if strings.Contains(h, "Not shown") {
		t.Error("more than three reviews on the home page")
	}

	// Other settings, other facts: pay on the day only, no free cancellation, deposits at handover.
	varied := newExampleEnv(t, "reservation-uae", func(def *appdef.App) {
		b := def.Seed["business"][0]
		b["card_enabled"], b["free_cancel_hours"], b["deposit_hold_enabled"], b["days_ahead"] = 0, 0, 0, 30
	})
	v := varied.get("/", "").Body
	for _, want := range []string{"<dd>Pay on the day</dd>", "Up to 30 days ahead", "Refundable, paid at handover", "You pay on the day; nothing is charged online.</p>",
		"Message us on WhatsApp and we will help.", "paid at handover. The amount is on each rental"} {
		if !strings.Contains(v, want) {
			t.Errorf("varied boutique home lacks %q", want)
		}
	}
	for _, not := range []string{"by card to reserve", "<dt>Cancellation</dt>", "Held on your card"} {
		if strings.Contains(v, not) {
			t.Errorf("varied boutique home has %q", not)
		}
	}

	// Any other font keeps the usual layout.
	plain := newExampleEnv(t, "reservation-uae", func(def *appdef.App) { def.Seed["business"][0]["font"] = "modern" })
	p := plain.get("/", "").Body
	if strings.Contains(p, `class="r-top`) || strings.Contains(p, "r-foot") || strings.Contains(p, "hdr-cta") || strings.Contains(p, `class="r-out"`) ||
		!strings.Contains(p, `class="intro`) || !strings.Contains(p, `class="tiles"`) || !strings.Contains(p, "From AED 150.00") {
		t.Error("modern font shows boutique sections")
	}
	if c := plain.get("/?c=Cars", "").Body; strings.Contains(c, "Dhow") || !strings.Contains(c, "Nissan Sunny") {
		t.Error("modern font: category filter")
	}
}
