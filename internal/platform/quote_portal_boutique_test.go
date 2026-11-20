package platform_test

import (
	"strings"
	"testing"

	"github.com/arif-rachim/renoxium/internal/appdef"
)

// The boutique theme of quote-portal-uae: navy and gold, a photo hero with a
// licence badge (or the years in business, only when the owner set them), a
// strip of highlights, about with the service areas, every package in one
// row with arrows, four steps, a custom-case band and a three-column footer
// when business.font is boutique; the usual layout for every other font.
// Invoices and printouts stay on white paper (checked in a browser: test
// responses shorten style blocks).
func TestQuotePortalBoutiqueTheme(t *testing.T) {
	e := newExampleEnv(t, "quote-portal-uae", nil)
	home := e.get("/", "").Body
	for _, want := range []string{`data-font="boutique"`, "--brand:#c9a14a", `class="s-hero`, "Business setup in Dubai",
		`<h1 id="hero-h">Your company in Dubai, <span class="accent">set up clearly.</span></h1>`, `class="s-links"`, `href="/#how"`,
		`class="s-badge"`, "Licensed in the UAE", "DET trade licence 1234567", `class="s-strip"`, "Company formation", "Visas and PRO", "Tax and VAT", "Clear quotes",
		"Clear advice. Clear prices.", "Our expertise", `id="s-track"`, `data-scroll="1"`, `class="s-card" href="/packages/1"`, "Monthly bookkeeping",
		`id="how"`, "<span class=\"s-num\">04</span>", "Something else?", `class="site-foot b-foot" id="contact"`, "Quick links"} {
		if !strings.Contains(home, want) {
			t.Errorf("boutique home lacks %q", want)
		}
	}
	if strings.Contains(home, "Years in business") {
		t.Error("boutique home shows years in business although the owner set none")
	}
	ar := e.get("/?lang=ar", "").Body
	for _, want := range []string{`dir="rtl"`, "تأسيس الأعمال في دبي", `<span class="accent">بتأسيس واضح.</span>`, "مرخّصون في الإمارات", "نصيحة واضحة. أسعار واضحة.",
		"خبراتنا", `href="/packages/1?lang=ar"`, "كيف نعمل", "روابط سريعة"} {
		if !strings.Contains(ar, want) {
			t.Errorf("Arabic boutique home lacks %q", want)
		}
	}
	// The other pages keep their layout, with the boutique look and footer.
	if s := e.get("/packages/1", "").Body; !strings.Contains(s, `class="site-foot b-foot"`) || !strings.Contains(s, "Mainland LLC company setup") || strings.Contains(s, `class="s-hero`) {
		t.Error("package page in boutique")
	}

	// The years in business, when the owner set them, replace the licence badge.
	years := newExampleEnv(t, "quote-portal-uae", func(def *appdef.App) { def.Seed["business"][0]["years_in_business"] = 12 })
	if h := years.get("/", "").Body; !strings.Contains(h, "<strong>12+</strong><span>Years in business</span>") || strings.Contains(h, "Licensed in the UAE") {
		t.Error("years in business badge")
	}

	// Any other font keeps the usual layout.
	plain := newExampleEnv(t, "quote-portal-uae", func(def *appdef.App) { def.Seed["business"][0]["font"] = "modern" })
	if p := plain.get("/", "").Body; strings.Contains(p, `class="s-hero`) || strings.Contains(p, `class="b-foot`) || strings.Contains(p, `class="s-links"`) || !strings.Contains(p, `class="intro`) {
		t.Error("modern font shows boutique sections")
	}
}
