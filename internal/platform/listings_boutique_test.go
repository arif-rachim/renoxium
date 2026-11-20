package platform_test

import (
	"strings"
	"testing"

	"github.com/arif-rachim/renoxium/internal/appdef"
)

// The boutique theme of listings-uae: a photo hero with a floating search
// bar, about with highlights, a row of property cards with arrows, agents, a
// valuation band and a four-column footer when agency.font is boutique; the
// usual layout for every other font. Client reviews are shown only when the
// owner adds real ones; the seed has none.
func TestListingsBoutiqueTheme(t *testing.T) {
	e := newExampleEnv(t, "listings-uae", nil)
	home := e.get("/", "").Body
	for _, want := range []string{`data-font="boutique"`, "--brand:#1f3a2e", `class="v-hero`, "Homes to buy and rent in Dubai",
		`<h1 id="hero-h">Homes in Dubai, <span class="accent">known street by street.</span></h1>`, `class="btn hdr-cta"`,
		`<form class="v-find" method="get" action="/search"`, `name="purpose"`, `name="community"`, `<option value="Dubai Marina">`, `name="type"`, `name="max"`,
		"Real estate, the personal way.", "Licensed brokers", "A permit on every advert", "Viewings at your time",
		`id="v-track"`, `data-scroll="1"`, `class="v-card" href="/l/1"`, "MH-1001", "AED 2,350,000", "2 bedrooms", "Omar Al Hashimi",
		`class="v-sell"`, `class="site-foot b-foot"`, "Property types", `href="/search?type=villa"`} {
		if !strings.Contains(home, want) {
			t.Errorf("boutique home lacks %q", want)
		}
	}
	for _, unwanted := range []string{`class="v-reviews"`, "MH-1009", `class="intro`} {
		if strings.Contains(home, unwanted) {
			t.Errorf("boutique home shows %q", unwanted)
		}
	}
	ar := e.get("/?lang=ar", "").Body
	for _, want := range []string{`dir="rtl"`, "منازل للبيع والإيجار في دبي", `<span class="accent">نعرفها شارعاً شارعاً.</span>`, "عقارات بلمسة شخصية.",
		"وسطاء مرخّصون", "عقاراتنا", `href="/l/1?lang=ar"`, `name="lang" value="ar"`, "أنواع العقارات", "تصفح العقارات"} {
		if !strings.Contains(ar, want) {
			t.Errorf("Arabic boutique home lacks %q", want)
		}
	}
	// The other pages keep their layout, with the boutique look and footer.
	if s := e.get("/search?purpose=rent", "").Body; !strings.Contains(s, `class="site-foot b-foot"`) || !strings.Contains(s, "MH-1002") || strings.Contains(s, `class="v-hero`) {
		t.Error("search page in boutique")
	}

	// Real client reviews, added by the owner, are shown (three at most).
	rated := newExampleEnv(t, "listings-uae", func(def *appdef.App) {
		var rows []map[string]any
		for i, n := range []string{"sara M.", "Karim B.", "Fatima Z.", "Not shown"} {
			rows = append(rows, map[string]any{"quote": "Quote of " + n, "quote_ar": "رأي", "name": n, "area": "Dubai Hills", "position": i + 1, "active": 1})
		}
		def.Seed["testimonials"] = rows
	})
	h := rated.get("/", "").Body
	for _, want := range []string{`class="v-reviews"`, "What our clients say", "Quote of sara M.", "<strong>Karim B.</strong>", "Quote of Fatima Z.", "Dubai Hills", `aria-hidden="true">S</span>`} {
		if !strings.Contains(h, want) {
			t.Errorf("home with reviews lacks %q", want)
		}
	}
	if strings.Contains(h, "Not shown") {
		t.Error("more than three reviews on the home page")
	}

	// Any other font keeps the usual layout.
	plain := newExampleEnv(t, "listings-uae", func(def *appdef.App) { def.Seed["agency"][0]["font"] = "modern" })
	p := plain.get("/", "").Body
	if strings.Contains(p, `class="v-hero`) || strings.Contains(p, `class="b-foot`) || strings.Contains(p, "hdr-cta") || !strings.Contains(p, `class="intro`) {
		t.Error("modern font shows boutique sections")
	}
}
