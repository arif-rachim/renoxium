package platform_test

import (
	"strings"
	"testing"

	"github.com/arif-rachim/renoxium/internal/appdef"
)

// The boutique theme of home-service-uae: a split hero with a large
// headline, service cards with round icons, highlights (why choose us), three
// steps, what to know before booking, a brand-colour closing band and a
// three-column footer when business.font is boutique; the usual layout for
// every other font. A rating and a customer review are shown only when the
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
		`class="site-foot b-foot"`, "Quick links"} {
		if !strings.Contains(home, want) {
			t.Errorf("boutique home lacks %q", want)
		}
	}
	for _, unwanted := range []string{`class="proof"`, `class="h-quote"`, "has-quote", `class="intro`} {
		if strings.Contains(home, unwanted) {
			t.Errorf("boutique home shows %q although the owner set no rating or review", unwanted)
		}
	}
	ar := e.get("/?lang=ar", "").Body
	for _, want := range []string{`dir="rtl"`, "تنظيف · تكييف · صيانة", `<h1>عناية بمنزلك، <span class="accent">كما يجب.</span></h1>`, "كل ما يحتاجه منزلك.",
		"فرق مدرَّبة ومؤمَّن عليها", "عناية يمكنك الاعتماد عليها.", "ثلاث خطوات بسيطة.", "نصل إليك", "منزلك في أيدٍ أمينة.", "احجز زيارة",
		`href="/s/1?lang=ar"`, "تنظيف المنزل"} {
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
			"area": "Dubai Marina", "area_ar": "دبي مارينا", "position": 1, "active": 1}}
	})
	h := rated.get("/", "").Body
	for _, want := range []string{"4.9/5 from 132 reviews", `class="h-quote"`, "On time and spotless.", "<strong>sara M.</strong>", "Dubai Marina", `aria-hidden="true">S</span>`} {
		if !strings.Contains(h, want) {
			t.Errorf("rated home lacks %q", want)
		}
	}
	if a := rated.get("/?lang=ar", "").Body; !strings.Contains(a, "في الموعد ونظيف تماماً.") || !strings.Contains(a, "دبي مارينا") || !strings.Contains(a, "4.9 من 5 من 132 تقييماً") {
		t.Error("Arabic review or rating")
	}

	// Any other font: the usual layout, no boutique sections.
	modern := newExampleEnv(t, "home-service-uae", func(def *appdef.App) { def.Seed["business"][0]["font"] = "modern" })
	m := modern.get("/", "").Body
	for _, unwanted := range []string{`class="h-hero"`, `class="h-card"`, `class="h-why"`, `class="h-steps"`, `class="h-band"`, `class="btn hdr-cta"`, `b-foot"`, `class="h-nav"`} {
		if strings.Contains(m, unwanted) {
			t.Errorf("modern font shows %q", unwanted)
		}
	}
	if !strings.Contains(m, `class="intro`) || !strings.Contains(m, `class="tile"`) {
		t.Error("modern font lost its layout")
	}
}
