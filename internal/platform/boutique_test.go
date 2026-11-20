package platform_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/arif-rachim/renoxium/examples"
	"github.com/arif-rachim/renoxium/internal/appdef"
	"github.com/arif-rachim/renoxium/internal/platform"
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
	// The dense blocks: numbers counted from the tables, facts from the settings row, category chips with counts, the
	// team with what each specialist does, the week's opening hours and the footer's categories and hours.
	for _, want := range []string{`<a href="#services"><b>6</b><span>Services</span></a>`, `<a href="#services"><b>3</b><span>Categories</span></a>`,
		`<a href="#team"><b>3</b><span>Specialists</span></a>`, `<a href="#classes"><b>2</b><span>Classes</span></a>`,
		`class="b-fl"`, `<bdi dir="ltr">10:00 – 21:00</bdi><small>Every day</small>`, "Villa 14, Al Wasl Road, Jumeirah 1, Dubai",
		"50% deposit by card<small>Pay at the venue</small>", "Until 4 hours before your visit", "Up to 30 days ahead<small>A time every 30 min</small>",
		`data-cat="0" aria-current="true">All<b>6</b></a>`, `<a href="#cat-3" data-cat="3">Face &amp; Body<b>2</b></a>`,
		`data-cat="2" id="cat-2"`, `style="--f:2"`, "Not sure what to book?", `<span class="b-dur">60 min</span><span class="price">AED 150.00</span>`,
		`<span class="b-av" aria-hidden="true">P</span><span class="b-who"><strong>Priya</strong><span>Hair · Nails · Face &amp; Body</span></span><span class="b-n">5 services</span>`,
		`<strong>Layla</strong><span>Hair</span>`, `class="b-hours"`, `class="is-today"`, `<a href="/#cat-2">Nails<span class="b-fn">2</span></a>`,
		`<p class="b-fh"><span>Every day</span><bdi dir="ltr">10:00 – 21:00</bdi></p>`} {
		if !strings.Contains(home, want) {
			t.Errorf("boutique home lacks %q", want)
		}
	}
	if n := strings.Count(home, `<tr`); n != 7 {
		t.Errorf("the opening hours table has %d rows, want 7", n)
	}
	// Photos are pebbles: no arch (round top, square bottom) and no reviews without real ones.
	for _, banned := range []string{`well arch`, `class="arch`, ` arch"`, `oval"`, `class="b-reviews"`, "(sample)"} {
		if strings.Contains(home, banned) {
			t.Errorf("boutique home has %q", banned)
		}
	}
	ex, _ := examples.Get("booking-uae")
	if src := string(ex.JSON); !strings.Contains(src, "--peb-1:58% 42% 55% 45%/50% 56% 44% 50%") || !strings.Contains(src, "[dir=rtl]{--peb-1:42% 58% 45% 55%/56% 50% 50% 44%") ||
		strings.Contains(src, "999px 999px 0 0") || strings.Contains(src, "well arch") {
		t.Error("pebble shapes or their RTL mirror are missing, or an arch is left")
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

	// Real reviews (at most three), closed days, a specialist without listed services, pay at the venue only.
	varied := newExampleEnv(t, "booking-uae", func(def *appdef.App) {
		rows := []map[string]any{}
		for _, n := range []string{"Sara M.", "Huda", "Mona", "Dina"} {
			rows = append(rows, map[string]any{"quote": "Calm and careful, " + n + ".", "quote_ar": "هدوء وعناية.", "name": n, "area": "Jumeirah", "active": 1})
		}
		def.Seed["testimonials"] = rows
		biz := def.Seed["business"][0]
		biz["closed_days"], biz["card_enabled"], biz["cancellation_hours"] = "5, 6", 0, 0
		def.Seed["staff"][0]["services"] = ""
	})
	vh := varied.get("/", "").Body
	for _, want := range []string{`class="b-reviews"`, "Calm and careful, Sara M.", `<span class="b-av" aria-hidden="true">S</span><span class="b-who"><strong>Sara M.</strong><span>Jumeirah</span>`,
		"Calm and careful, Mona.", "<small>Closed: Friday, Saturday</small>", "<td>Closed</td>", ">Pay at the venue</dd>",
		`<strong>Layla</strong><span>Hair · Nails · Face &amp; Body</span></span><span class="b-n">6 services</span>`,
		`<p class="b-fh"><span>Sun – Thu</span><bdi dir="ltr">10:00 – 21:00</bdi></p><p class="b-fh"><span>Fri – Sat</span><span>Closed</span></p>`} {
		if !strings.Contains(vh, want) {
			t.Errorf("varied boutique home lacks %q", want)
		}
	}
	for _, not := range []string{"Calm and careful, Dina.", "deposit by card", "Free cancellation"} {
		if strings.Contains(vh, not) {
			t.Errorf("varied boutique home has %q", not)
		}
	}
	if a := varied.get("/?lang=ar", "").Body; !strings.Contains(a, "هدوء وعناية.") || !strings.Contains(a, "مغلق: الجمعة، السبت") && !strings.Contains(a, "مغلق: الجمعة, السبت") {
		t.Error("Arabic reviews or closed days")
	}

	// Any other font: the usual layout, no boutique sections.
	classic := newExampleEnv(t, "booking-uae", func(def *appdef.App) { def.Seed["business"][0]["font"] = "classic" })
	h := classic.get("/", "").Body
	if strings.Contains(h, "b-hero") || strings.Contains(h, "hdr-cta\"") || strings.Contains(h, "b-foot\"") || strings.Contains(h, `class="b-fl"`) ||
		strings.Contains(h, `class="b-chips"`) || strings.Contains(h, "b-fh") || !strings.Contains(h, `class="intro`) {
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

// store-uae's boutique home: collections as rounds, product badges and quick
// add, a banner only for a promoted coupon, a client quote only when the
// owner added a real one, and the luxury layout for the luxury fonts.
func TestStoreBoutiqueTheme(t *testing.T) {
	// The demo's eight products: the template's four and the demo's four more.
	e := newExampleEnv(t, "store-uae", func(def *appdef.App) {
		def.Seed["products"] = append(def.Seed["products"], platform.DemoExtraRows("store-uae", "products")...)
	})
	home := e.get("/", "").Body
	for _, want := range []string{`data-font="boutique"`, `class="e-hero"`, "Premium perfume collection", `<em class="accent">for every moment</em>`,
		`class="cats e-fams"`, `<span class="e-fam-name">Musk</span>`, "Featured fragrances", `<span class="e-badge">Best seller</span>`,
		`<span class="e-badge">New</span>`, `data-add="2"`, "10% off", "With the code WELCOME10 on orders over AED", "Crafted with passion",
		`class="site-foot b-foot"`, "--brand:#1f3a2e",
		// Numbers counted from the products, facts from the shop settings.
		`<a href="#products"><strong>8</strong><span>products</span></a>`, `<a href="#families"><strong>4</strong><span>collections</span></a>`,
		`<strong>AED 60</strong><span>prices from</span>`, `<strong>AED 15 to all 7 emirates</strong>`, `<strong>On orders over AED 200</strong>`,
		`<strong>Card payment · Cash on delivery</strong>`, `<strong>Includes VAT (5%)</strong>`,
		// Collections with their product count, new arrivals, the few-left note, footer category links.
		`<span class="e-fam-name">Oud</span><span class="e-fam-n">2</span>`, `id="new-h">New arrivals</h2>`, `<span class="e-stock">Only 4 left</span>`,
		`<a href="/?c=Gifts#products">Gifts</a>`, "Shop 12, Gold Souk, Deira, Dubai</strong>"} {
		if !strings.Contains(home, want) {
			t.Errorf("boutique store home lacks %q", want)
		}
	}
	// The arch frame is gone: ovals, circles and squircles only.
	if strings.Contains(home, ` arch"`) || strings.Contains(home, `"arch`) || strings.Contains(home, " arch ") || strings.Contains(home, "e-arch") {
		t.Error("the boutique store home still uses the arch frame")
	}
	if strings.Contains(home, "Made to order") || strings.Count(home, `class="e-new-card"`) != 4 {
		t.Error("an order-ahead fact without preorder_days, or not four new arrivals")
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
	if f := e.get("/?c=Musk", "").Body; !strings.Contains(f, `aria-current="page"><span class="e-round">`) || strings.Contains(f, "Royal Oud</span>") ||
		strings.Contains(f, `class="e-new"`) || !strings.Contains(f, `<strong>8</strong><span>products</span>`) {
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

	// Settings drive the facts strip: no fee, cash only, made to order; at most three quotes; four products have no new arrivals row.
	made := newExampleEnv(t, "store-uae", func(def *appdef.App) {
		st := def.Seed["store"][0]
		st["delivery_fee"], st["card_enabled"], st["preorder_days"] = 0, 0, 2
		def.Seed["products"] = def.Seed["products"][:4]
		for _, n := range []string{"One", "Two", "Three", "Four"} {
			def.Seed["testimonials"] = append(def.Seed["testimonials"], map[string]any{"quote": "Quote " + n, "name": n, "active": 1})
		}
	})
	m := made.get("/", "").Body
	for _, want := range []string{"<strong>Free to all 7 emirates</strong>", "<strong>Cash on delivery</strong>", "<strong>Order at least 2 days ahead</strong>"} {
		if !strings.Contains(m, want) {
			t.Errorf("facts strip lacks %q", want)
		}
	}
	if strings.Contains(m, "On orders over") || strings.Contains(m, "Card payment") || strings.Count(m, `class="e-quote"`) != 3 || strings.Contains(m, `class="e-new"`) {
		t.Error("facts strip ignores the settings, more than three quotes, or new arrivals for four products")
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
