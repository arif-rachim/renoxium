package platform_test

import (
	"strings"
	"testing"

	"github.com/arif-rachim/renoxium/internal/appdef"
)

// The boutique theme of quote-portal-uae: navy and gold, a photo hero with a
// diagonal lower edge and a licence badge, a strip of highlights, numbers
// counted from the packages and the settings (the years in business only
// when the owner set them), about with the service areas, every package in
// one row with arrows and its typical duration, four steps, what every quote
// includes and the usual questions (both from the settings), client reviews
// only when there are any, a custom-case band and a four-column footer when
// business.font is boutique; the usual layout for every other font.
// Invoices and printouts stay on white paper (checked in a browser: test
// responses shorten style blocks).
func TestQuotePortalBoutiqueTheme(t *testing.T) {
	e := newExampleEnv(t, "quote-portal-uae", nil)
	home := e.get("/", "").Body
	for _, want := range []string{`data-font="boutique"`, "--brand:#c9a14a", `class="s-hero`, "Business setup in Dubai",
		`<h1 id="hero-h">Your company in Dubai, <span class="accent">set up clearly.</span></h1>`, `class="s-links"`, `href="/#how"`,
		`class="s-badge"`, "Licensed in the UAE", "DET trade licence 1234567", `class="s-strip"`, "Company formation", "Visas and PRO", "Tax and VAT", "Clear quotes",
		"Clear advice. Clear prices.", "Our expertise", `id="s-track"`, `data-scroll="1"`, `class="s-card" href="/packages/1"`, "Monthly bookkeeping",
		`id="how"`, "<span class=\"s-num\">04</span>", "Something else?", `class="site-foot b-foot" id="contact"`, "Quick links",
		// The diagonal hero edge, and numbers counted from the packages and taken from the settings.
		`<svg class="s-cut"`, `<a href="#services"><strong>6</strong><span>Services</span></a>`, `<a href="#areas"><strong>3</strong><span>Service areas</span></a>`,
		`<a href="#faq"><strong>14</strong><span>Days a quote stays valid</span></a>`, `<a href="#faq"><strong>50%</strong><span>Deposit when you accept</span></a>`,
		`<strong>Company formation</strong><small>2 services · From AED`, `</svg><span class="sr">Typical duration: </span>2 to 3 weeks</span>`,
		// What every quote includes and the questions, from the settings.
		"Included in every quote", "Government fees and our own fee, listed line by line", "5% VAT shown as its own line",
		"50% deposit when you accept, the balance when the work is done", "The price held for 14 days", "Payment by card or bank transfer",
		`<details open><summary>How much do I pay up front?</summary><p>A deposit of 50% when you accept the quote.`, "14 days from the day we send it",
		"By card online or by bank transfer.", "No. Starting prices are shown before VAT",
		`<h2 id="fs-h">Services</h2><a href="/packages/1">Mainland LLC company setup</a>`, "Chat on WhatsApp <bdi dir=\"ltr\">&#43;971500000000</bdi>"} {
		if !strings.Contains(home, want) {
			t.Errorf("boutique home lacks %q", want)
		}
	}
	for _, not := range []string{"Years in business", `class="s-reviews"`, "999px 999px", "(sample)"} {
		if strings.Contains(home, not) {
			t.Errorf("boutique home has %q", not)
		}
	}
	ar := e.get("/?lang=ar", "").Body
	for _, want := range []string{`dir="rtl"`, "تأسيس الأعمال في دبي", `<span class="accent">بتأسيس واضح.</span>`, "مرخّصون في الإمارات", "نصيحة واضحة. أسعار واضحة.",
		"خبراتنا", `href="/packages/1?lang=ar"`, "كيف نعمل", "روابط سريعة",
		"<strong>6</strong><span>خدمات</span>", "ما يتضمنه كل عرض سعر", "دفعة مقدمة 50% عند القبول والباقي عند إنجاز العمل", "ما مدة صلاحية عرض السعر؟"} {
		if !strings.Contains(ar, want) {
			t.Errorf("Arabic boutique home lacks %q", want)
		}
	}
	// The other pages keep their layout, with the boutique look and footer.
	if s := e.get("/packages/1", "").Body; !strings.Contains(s, `class="site-foot b-foot"`) || !strings.Contains(s, "Mainland LLC company setup") || strings.Contains(s, `class="s-hero`) {
		t.Error("package page in boutique")
	}

	// The owner's settings change the numbers, the list and the answers: years in business, no deposit, prices including
	// VAT, bank transfer only; client reviews show once there are rows, three at most.
	set := newExampleEnv(t, "quote-portal-uae", func(def *appdef.App) {
		b := def.Seed["business"][0]
		b["years_in_business"], b["deposit_percent"], b["quote_valid_days"], b["prices_include_vat"], b["card_enabled"] = 12, 0, 30, 1, 0
		for _, n := range []string{"Aisha", "Bilal", "Carla", "Dana"} {
			def.Seed["testimonials"] = append(def.Seed["testimonials"], map[string]any{"quote": "Clear from " + n + ".", "name": n, "company": "Founder", "active": 1})
		}
	})
	h := set.get("/", "").Body
	for _, want := range []string{`<a href="#about"><strong>12</strong><span>Years in business</span></a>`, "Licensed in the UAE", "<strong>30</strong>", "The price held for 30 days",
		"Prices that already include 5% VAT", "Payment by bank transfer<", "Nothing. We ask for payment when the work is done.", "Yes. The prices on this site",
		`class="s-reviews"`, `<blockquote class="s-quote"><p>Clear from Aisha.</p><footer><strong>Aisha</strong><span>Founder</span></footer>`, "Clear from Carla."} {
		if !strings.Contains(h, want) {
			t.Errorf("boutique home with other settings lacks %q", want)
		}
	}
	for _, not := range []string{"Deposit when you accept", "deposit when you accept", "By card online", "Payment by card", "Clear from Dana."} {
		if strings.Contains(h, not) {
			t.Errorf("boutique home with other settings has %q", not)
		}
	}

	// Any other font keeps the usual layout.
	plain := newExampleEnv(t, "quote-portal-uae", func(def *appdef.App) { def.Seed["business"][0]["font"] = "modern" })
	if p := plain.get("/", "").Body; strings.Contains(p, `class="s-hero`) || strings.Contains(p, `class="b-foot`) || strings.Contains(p, `class="s-links"`) || !strings.Contains(p, `class="intro`) {
		t.Error("modern font shows boutique sections")
	}
}
