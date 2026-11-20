package platform_test

import (
	"strings"
	"testing"

	"github.com/arif-rachim/renoxium/internal/appdef"
)

// The boutique theme of home-service-uae: a split hero with a large
// headline, numbers counted from the tables and a strip of facts from the
// settings, compact service cards with round icons, highlights (why choose
// us) beside three steps, the areas as chips with their surcharge, a short
// FAQ built from the settings, a brand-colour closing band and a footer with
// the areas when business.font is boutique; the usual layout for every other
// font. A rating and up to three customer reviews are shown only when the
// owner set real ones; the seed has none.
func TestHomeServiceUAEBoutiqueTheme(t *testing.T) {
	e := newExampleEnv(t, "home-service-uae", nil)
	home := e.get("/", "").Body
	for _, want := range []string{`data-font="boutique"`, "--brand:#1f3a2e", "Gilda+Display", `class="h-hero"`, "Cleaning · AC · Handyman",
		`<h1>Home care, <span class="accent">done right.</span></h1>`, "Trained, insured crews for cleaning", `class="btn hdr-cta"`, `class="h-nav"`,
		`class="h-card" href="/s/1"`, `<span class="h-badge"><svg class="ico"`, "AED 40.00", "Pest control (apartment)", "Everything your home needs.",
		`id="why"`, "Care you can count on.", "Trained, insured crews", "Clear prices", "On time", "Pay your way",
		`id="process"`, "Three simple steps.", "Book online", "We arrive", "Job done",
		"<span>Arabian Ranches</span><span class=\"price\">+ AED 25.00</span>", `class="h-band"`, "Your home, in good hands.",
		`class="site-foot b-foot"`, "Quick links",
		// Counted from the tables, and facts from the settings row.
		`<a href="#services"><strong>6</strong><span>Services</span></a>`, `<a href="#areas"><strong>5</strong><span>Areas served</span></a>`,
		`<span class="h-count"><strong>2</strong><span>Crews</span></span>`, `class="h-strip"`, "<strong>Hours</strong>Visits 08:00–20:00, closed on Sundays",
		"<strong>Payment</strong>Card online · Cash on completion", "<strong>Cancelling</strong>Free up to 4 hours before the visit",
		"<strong>Prices</strong>Prices include VAT (5%)", "<strong>Minimum booking</strong>AED", "From AED 80.00 per visit",
		`<span>Dubai Marina</span><span class="h-free">No surcharge</span>`, `id="areas"`, "We cover 5 areas.",
		"<details open><summary>Which areas do you cover?</summary><p>We cover Dubai Marina, JLT, Al Barsha, Downtown Dubai, Arabian Ranches. Some areas carry a surcharge",
		"<summary>How do I pay?</summary><p>Pay now on a secure page.", "<summary>Can I cancel a visit?</summary><p>Free cancellation until 4 hours before",
		"<summary>How far ahead can I book?</summary><p>You can book up to 14 days ahead.</p>",
		`<p class="b-foot-areas"><span>Dubai Marina</span>`} {
		if !strings.Contains(home, want) {
			t.Errorf("boutique home lacks %q", want)
		}
	}
	for _, unwanted := range []string{`class="proof"`, `class="h-quote"`, `class="h-rev"`, `class="intro`, "999px 999px", `class="arch`} {
		if strings.Contains(home, unwanted) {
			t.Errorf("boutique home shows %q although the owner set no rating or review", unwanted)
		}
	}
	ar := e.get("/?lang=ar", "").Body
	for _, want := range []string{`dir="rtl"`, "تنظيف · تكييف · صيانة", `<h1>عناية بمنزلك، <span class="accent">كما يجب.</span></h1>`, "كل ما يحتاجه منزلك.",
		"فرق مدرَّبة ومؤمَّن عليها", "عناية يمكنك الاعتماد عليها.", "ثلاث خطوات بسيطة.", "نصل إليك", "منزلك في أيدٍ أمينة.", "احجز زيارة",
		`href="/s/1?lang=ar"`, "تنظيف المنزل", "<span>مناطق نخدمها</span>", "بطاقة عبر الإنترنت · نقداً عند الانتهاء", "بلا رسوم إضافية", "ما المناطق التي تغطونها؟",
		"نغطي دبي مارينا، أبراج بحيرات جميرا،", "يمكنك الحجز حتى 14 يوماً مقدَّماً."} {
		if !strings.Contains(ar, want) {
			t.Errorf("Arabic boutique home lacks %q", want)
		}
	}
	// The other pages keep their layout and flows, with the boutique look and footer.
	if s := e.get("/s/1", "").Body; !strings.Contains(s, `class="site-foot b-foot"`) || !strings.Contains(s, `id="book"`) || strings.Contains(s, `class="h-hero"`) {
		t.Error("service page in boutique")
	}

	// A real rating and a real review, set by the owner, are shown.
	rated := newExampleEnv(t, "home-service-uae", func(def *appdef.App) {
		def.Seed["business"][0]["rating"] = 4.86
		def.Seed["business"][0]["review_count"] = 132
		def.Seed["testimonials"] = []map[string]any{{"quote": "On time and spotless.", "quote_ar": "في الموعد ونظيف تماماً.", "name": "sara M.",
			"area": "Dubai Marina", "area_ar": "دبي مارينا", "position": 1, "active": 1},
			{"quote": "Second review.", "name": "Ali", "position": 2, "active": 1}, {"quote": "Third review.", "name": "Bea", "position": 3, "active": 1},
			{"quote": "Fourth review.", "name": "Cem", "position": 4, "active": 1}, {"quote": "Hidden review.", "name": "Dan", "position": 0, "active": 0}}
	})
	h := rated.get("/", "").Body
	for _, want := range []string{"4.9/5 from 132 reviews", `class="h-quote"`, "On time and spotless.", "<strong>sara M.</strong>", "Dubai Marina", `aria-hidden="true">S</span>`,
		`class="h-rev"`, "What customers say", "Second review.", "Third review."} {
		if !strings.Contains(h, want) {
			t.Errorf("rated home lacks %q", want)
		}
	}
	if strings.Contains(h, "Fourth review.") || strings.Contains(h, "Hidden review.") || strings.Count(h, `class="h-quote"`) != 3 {
		t.Error("the home page shows up to three active reviews")
	}
	if a := rated.get("/?lang=ar", "").Body; !strings.Contains(a, "في الموعد ونظيف تماماً.") || !strings.Contains(a, "دبي مارينا") || !strings.Contains(a, "4.9 من 5 من 132 تقييماً") {
		t.Error("Arabic review or rating")
	}

	// Any other font: the usual layout, no boutique sections.
	modern := newExampleEnv(t, "home-service-uae", func(def *appdef.App) { def.Seed["business"][0]["font"] = "modern" })
	m := modern.get("/", "").Body
	for _, unwanted := range []string{`class="h-hero"`, `class="h-card"`, `class="h-why"`, `class="h-steps"`, `class="h-band"`, `class="btn hdr-cta"`, `b-foot"`, `class="h-nav"`,
		`class="h-strip"`, `class="h-counts"`, `class="h-faq"`} {
		if strings.Contains(m, unwanted) {
			t.Errorf("modern font shows %q", unwanted)
		}
	}
	if !strings.Contains(m, `class="intro`) || !strings.Contains(m, `class="tile"`) {
		t.Error("modern font lost its layout")
	}
}
