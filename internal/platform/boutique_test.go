package platform_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/arif-rachim/renoxium/examples"
	"github.com/arif-rachim/renoxium/internal/appdef"
)

// The boutique theme: booking-uae's own layout (split hero, highlights,
// arched cards, brand band, closing call to book, three-column footer) when
// business.font is boutique; the usual layout for every other font. A
// rating is shown only when the owner set a real one.
func TestBookingBoutiqueTheme(t *testing.T) {
	e := newExampleEnv(t, "booking-uae", nil)
	home := e.get("/", "").Body
	for _, want := range []string{`data-font="boutique"`, `class="b-hero"`, "Where calm meets care", `<span class="accent">Every visit.</span>`,
		"Hair, nails and skincare by people who take their time", "Gentle products", "Women only", `class="stamp"`, "Book online",
		`class="b-card"`, `class="b-cat">Face &amp; Body`, "Your time. Your glow.", "Ready for your visit?", `class="btn hdr-cta"`,
		`class="site-foot b-foot"`, "Quick links", "--brand:#b4643f", "Gilda+Display"} {
		if !strings.Contains(home, want) {
			t.Errorf("boutique home lacks %q", want)
		}
	}
	if strings.Contains(home, `class="proof"`) {
		t.Error("a rating is shown although the owner set none")
	}
	ar := e.get("/?lang=ar", "").Body
	for _, want := range []string{`dir="rtl"`, "حيث يلتقي الهدوء بالعناية", "في كل زيارة.", "منتجات لطيفة", "وقتكِ. إشراقتكِ."} {
		if !strings.Contains(ar, want) {
			t.Errorf("Arabic boutique home lacks %q", want)
		}
	}
	// Other pages keep their layout and get the boutique look and footer.
	if s := e.get("/s/1", "").Body; !strings.Contains(s, `class="site-foot b-foot"`) || !strings.Contains(s, `id="picker"`) {
		t.Error("service page in boutique")
	}

	// A real rating, set by the owner, is shown with its number of reviews.
	rated := newExampleEnv(t, "booking-uae", func(def *appdef.App) {
		def.Seed["business"][0]["rating"] = 4.86
		def.Seed["business"][0]["review_count"] = 132
	})
	if h := rated.get("/", "").Body; !strings.Contains(h, "4.9/5 from 132 reviews") {
		t.Error("rating not shown")
	}

	// Any other font: the usual layout, no boutique sections.
	classic := newExampleEnv(t, "booking-uae", func(def *appdef.App) { def.Seed["business"][0]["font"] = "classic" })
	h := classic.get("/", "").Body
	if strings.Contains(h, "b-hero") || strings.Contains(h, "hdr-cta\"") || strings.Contains(h, "b-foot") || !strings.Contains(h, `class="intro`) {
		t.Error("classic font shows boutique sections")
	}
}

// theme() and the boutique look are shared: every example carries the same
// lib theme and the same boutique template, so the boutique font works the
// same in every template.
func TestThemeIsTheSameEverywhere(t *testing.T) {
	var first string
	var ref struct {
		Lib       map[string]string
		Templates map[string]string
	}
	for _, ex := range examples.All() {
		var def struct {
			Lib       map[string]string
			Templates map[string]string
		}
		json.Unmarshal(ex.JSON, &def)
		if def.Lib["theme"] == "" {
			continue
		}
		if !strings.Contains(def.Templates["head"], `{{if .theme.boutique}}{{template "boutique" .}}{{end}}`) {
			t.Errorf("%s's head does not include the boutique template", ex.Name)
		}
		if first == "" {
			first, ref = ex.Name, def
			continue
		}
		if def.Lib["theme"] != ref.Lib["theme"] {
			t.Errorf("%s's lib theme differs from %s's", ex.Name, first)
		}
		if def.Templates["boutique"] != ref.Templates["boutique"] {
			t.Errorf("%s's boutique template differs from %s's", ex.Name, first)
		}
	}
}

// store-uae's boutique home: collections as arches, product badges and quick
// add, a banner only for a promoted coupon, a client quote only when the
// owner added a real one, and the luxury layout for the luxury fonts.
func TestStoreBoutiqueTheme(t *testing.T) {
	e := newExampleEnv(t, "store-uae", nil)
	home := e.get("/", "").Body
	for _, want := range []string{`data-font="boutique"`, `class="e-hero"`, "Premium perfume collection", `<em class="accent">for every moment</em>`,
		`class="cats e-fams"`, `<span class="e-fam-name">Musk</span>`, "Featured fragrances", `<span class="e-badge">Best seller</span>`,
		`<span class="e-badge">New</span>`, `data-add="2"`, "10% off", "With the code WELCOME10 on orders over AED", "Crafted with passion",
		`class="site-foot b-foot"`, "--brand:#1f3a2e"} {
		if !strings.Contains(home, want) {
			t.Errorf("boutique store home lacks %q", want)
		}
	}
	if strings.Contains(home, `data-add="1"`) {
		t.Error("quick add offered for a product with options")
	}
	if strings.Contains(home, `class="e-quote"`) {
		t.Error("a client quote is shown although none was added")
	}
	if ar := e.get("/?lang=ar", "").Body; !strings.Contains(ar, "مجموعة عطور فاخرة") || !strings.Contains(ar, "الأكثر مبيعاً") || !strings.Contains(ar, "مع الرمز WELCOME10") {
		t.Error("Arabic boutique store home")
	}
	// In-place filtering keeps working: the collections are the .cats the motion kit swaps.
	if f := e.get("/?c=Musk", "").Body; !strings.Contains(f, `aria-current="page"><span class="well arch">`) || strings.Contains(f, "Royal Oud</span>") {
		t.Error("collection filter")
	}

	quiet := newExampleEnv(t, "store-uae", func(def *appdef.App) {
		def.Seed["coupons"][0]["promote"] = 0
		def.Seed["testimonials"] = []map[string]any{{"quote": "The oud lasts all day.", "name": "Ayesha", "active": 1}}
	})
	h := quiet.get("/", "").Body
	if strings.Contains(h, `class="e-promo"`) || !strings.Contains(h, "The oud lasts all day.") || !strings.Contains(h, "— Ayesha") {
		t.Error("promo without a promoted coupon, or the client quote")
	}

	lux := newExampleEnv(t, "store-uae", func(def *appdef.App) { def.Seed["store"][0]["font"] = "heritage" })
	if l := lux.get("/", "").Body; strings.Contains(l, "e-hero") || !strings.Contains(l, `class="hero`) || strings.Contains(l, "b-foot") {
		t.Error("heritage font shows boutique sections")
	}
}

// A template that is a style block ends with it: rules added after
// </style> are page text, and the browser ignores them.
func TestStyleTemplatesEndWithTheirStyle(t *testing.T) {
	for _, ex := range examples.All() {
		var def struct{ Templates map[string]string }
		json.Unmarshal(ex.JSON, &def)
		for name, body := range def.Templates {
			body = strings.TrimSpace(body)
			if strings.HasPrefix(body, "<style") && !strings.HasSuffix(body, "</style>") {
				t.Errorf("%s: template %s has text after </style>", ex.Name, name)
			}
		}
	}
}
