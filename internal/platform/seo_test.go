package platform_test

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"regexp"
	"strings"
	"testing"

	"github.com/arif-rachim/renoxium/examples"
	"github.com/arif-rachim/renoxium/internal/appdef"
	"github.com/arif-rachim/renoxium/internal/platform"
)

func liveExample(t *testing.T, example, app string) *platform.Platform {
	t.Helper()
	p := newPlatform(t)
	ex, _ := examples.Get(example)
	def, problems := appdef.Parse(ex.JSON)
	if len(problems) > 0 {
		t.Fatal(problems)
	}
	if _, err := p.CreateApp("usr_test", app); err != nil {
		t.Fatal(err)
	}
	res, err := p.ApplyChange(context.Background(), app, def)
	if err != nil || !res.OK {
		t.Fatal(err, res)
	}
	for _, c := range res.Checks {
		if c.Status >= 500 {
			t.Errorf("check %s: %d %s", c.Route, c.Status, c.Error)
		}
	}
	if _, err := p.Publish(app, 0); err != nil {
		t.Fatal(err)
	}
	return p
}

var ldRE = regexp.MustCompile(`(?s)<script type="application/ld\+json">(.*?)</script>`)

// jsonLD returns the page's structured data, which must be valid JSON.
func jsonLD(t *testing.T, body string) map[string]any {
	t.Helper()
	m := ldRE.FindStringSubmatch(body)
	if m == nil {
		t.Fatalf("no structured data in the page")
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(m[1]), &out); err != nil {
		t.Fatalf("structured data is not JSON: %v\n%s", err, m[1])
	}
	return out
}

func wants(t *testing.T, what, body string, parts ...string) {
	t.Helper()
	for _, part := range parts {
		if !strings.Contains(body, part) {
			t.Errorf("%s: missing %s", what, part)
		}
	}
}

// A live store tells search engines, link previews and AI assistants what
// they need: robots.txt, a sitemap with every product in both languages,
// llms.txt, and a complete <head> on each page.
func TestStoreIsFoundAndShared(t *testing.T) {
	p := liveExample(t, "store-uae", "noor")
	const live, preview = "noor.renoxium.test", "noor--preview.renoxium.test"

	robots := do(t, p, "GET", live, "/robots.txt", nil)
	if robots.status != 200 || !strings.HasPrefix(robots.header.Get("Content-Type"), "text/plain") {
		t.Fatalf("robots.txt: %d %s", robots.status, robots.header.Get("Content-Type"))
	}
	wants(t, "robots.txt", robots.body, "User-agent: *", "Allow: /_rx/files/", "Disallow: /_rx/", "Disallow: /orders",
		"Sitemap: https://noor.renoxium.test/sitemap.xml")
	if strings.Contains(robots.body, "GPTBot") {
		t.Error("AI crawlers are blocked without the owner asking")
	}
	// The preview is never indexed.
	if r := do(t, p, "GET", preview, "/robots.txt", nil); r.body != "User-agent: *\nDisallow: /\n" || r.header.Get("X-Robots-Tag") != "noindex" {
		t.Errorf("preview robots.txt: %q", r.body)
	}

	sitemap := do(t, p, "GET", live, "/sitemap.xml", nil)
	var set struct {
		URLs []struct {
			Loc string `xml:"loc"`
		} `xml:"url"`
	}
	if err := xml.Unmarshal([]byte(sitemap.body), &set); err != nil {
		t.Fatalf("sitemap is not XML: %v", err)
	}
	locs := map[string]bool{}
	for _, u := range set.URLs {
		locs[u.Loc] = true
	}
	for _, want := range []string{"https://noor.renoxium.test/", "https://noor.renoxium.test/?lang=ar",
		"https://noor.renoxium.test/p/1", "https://noor.renoxium.test/p/1?lang=ar"} {
		if !locs[want] {
			t.Errorf("sitemap: %s is missing", want)
		}
	}
	for loc := range locs {
		if strings.Contains(loc, "/orders") || strings.Contains(loc, "/cart") || strings.Contains(loc, "/api/") {
			t.Errorf("sitemap lists %s", loc)
		}
	}
	wants(t, "sitemap", sitemap.body, `hreflang="ar" href="https://noor.renoxium.test/p/1?lang=ar"`, `hreflang="x-default"`)

	llms := do(t, p, "GET", live, "/llms.txt", nil)
	wants(t, "llms.txt", llms.body, "# Al Noor Perfumes", "> Oud, musk and amber from the UAE",
		"](https://noor.renoxium.test/p/1)", "https://noor.renoxium.test/mcp", "- search_products: ")

	home := do(t, p, "GET", live, "/?utm_source=instagram&fbclid=x", nil)
	wants(t, "home", home.body,
		`<link rel="canonical" href="https://noor.renoxium.test/">`,
		`<meta name="description" content="Oud, musk and amber from the UAE">`,
		`<meta property="og:title" content="Al Noor Perfumes">`,
		`<meta property="og:description" content="Oud, musk and amber from the UAE">`,
		`<meta property="og:url" content="https://noor.renoxium.test/">`,
		`<meta property="og:site_name" content="Al Noor Perfumes">`,
		`<meta name="twitter:card"`,
		`<link rel="alternate" hreflang="en" href="https://noor.renoxium.test/">`,
		`<link rel="alternate" hreflang="ar" href="https://noor.renoxium.test/?lang=ar">`,
		`<link rel="alternate" hreflang="x-default" href="https://noor.renoxium.test/">`)
	if ld := jsonLD(t, home.body); ld["@type"] != "Store" || ld["name"] != "Al Noor Perfumes" || ld["url"] != "https://noor.renoxium.test/" {
		t.Errorf("home structured data: %v", ld)
	}

	// A filter stays in the canonical address; the Arabic page is its own.
	if r := do(t, p, "GET", live, "/?lang=ar&c=Oud", nil); !strings.Contains(r.body, `<link rel="canonical" href="https://noor.renoxium.test/?c=Oud&amp;lang=ar">`) {
		t.Errorf("arabic canonical is missing")
	}

	product := do(t, p, "GET", live, "/p/1", nil)
	wants(t, "product", product.body, `<link rel="canonical" href="https://noor.renoxium.test/p/1">`, `<meta property="og:title" content="Royal Oud | Al Noor Perfumes">`)
	ld := jsonLD(t, product.body)
	offer, _ := ld["offers"].(map[string]any)
	if ld["@type"] != "Product" || ld["name"] != "Royal Oud" || offer["price"] != "250.00" || offer["priceCurrency"] != "AED" ||
		offer["availability"] != "https://schema.org/InStock" {
		t.Errorf("product structured data: %v", ld)
	}

	// The preview and pages behind sign-in get none of it.
	if r := do(t, p, "GET", preview, "/", nil); strings.Contains(r.body, `rel="canonical"`) {
		t.Error("the preview has a canonical address")
	}
}

// Every gallery example serves a sitemap whose queries work, and pages
// whose structured data is valid JSON.
func TestExamplesAreFound(t *testing.T) {
	for _, ex := range examples.Gallery() {
		t.Run(ex.Name, func(t *testing.T) {
			p := liveExample(t, ex.Name, "shop")
			sitemap := do(t, p, "GET", "shop.renoxium.test", "/sitemap.xml", nil)
			if sitemap.status != 200 || strings.Count(sitemap.body, "<url>") < 2 {
				t.Fatalf("sitemap: %d %s", sitemap.status, sitemap.body)
			}
			home := do(t, p, "GET", "shop.renoxium.test", "/", nil)
			wants(t, "home", home.body, `<meta name="description" content="`, `<link rel="canonical" href="https://shop.renoxium.test/">`,
				`hreflang="ar"`, `property="og:title"`)
			if ld := jsonLD(t, home.body); ld["@context"] != "https://schema.org" || ld["name"] == "" {
				t.Errorf("home structured data: %v", ld)
			}
			// The first detail page in the sitemap describes its item.
			m := regexp.MustCompile(`<loc>https://shop\.renoxium\.test(/[a-z]+/\d+)</loc>`).FindStringSubmatch(sitemap.body)
			if m == nil {
				return // an example without detail pages
			}
			detail := do(t, p, "GET", "shop.renoxium.test", m[1], nil)
			if detail.status != 200 {
				t.Fatalf("%s: %d", m[1], detail.status)
			}
			if ld := jsonLD(t, detail.body); ld["name"] == "" || ld["url"] != "https://shop.renoxium.test"+m[1] {
				t.Errorf("%s structured data: %v", m[1], ld)
			}
		})
	}
}

// An app without a seo section still gets the files and the tags, the
// owner's choices are followed, and its own routes win.
func TestSEOSettings(t *testing.T) {
	p := newPlatform(t)
	ctx := context.Background()
	def := catalog(t)
	def.SEO = &appdef.SEO{Description: "Perfumes from Deira.", Image: "/_rx/files/share.png", AICrawlers: "block",
		Verify:  map[string]string{"google": "abc-123"},
		Sitemap: []string{"SELECT '/products/' || id AS path, name AS title FROM products ORDER BY id", "SELECT name FROM products"}}
	p.CreateApp("usr_test", "al-noor")
	res, err := p.ApplyChange(ctx, "al-noor", def)
	if err != nil || !res.OK {
		t.Fatal(err, res)
	}
	// The building agent hears about the query that returns no path.
	found := false
	for _, c := range res.Checks {
		found = found || (c.Route == "GET /sitemap.xml" && strings.Contains(c.Error, "seo.sitemap 1") && strings.Contains(c.Error, "path"))
	}
	if !found {
		t.Errorf("the broken sitemap query was not reported: %+v", res.Checks)
	}
	if _, err := p.Publish("al-noor", 0); err != nil {
		t.Fatal(err)
	}
	robots := do(t, p, "GET", liveHost, "/robots.txt", nil)
	wants(t, "robots.txt", robots.body, "User-agent: GPTBot\n", "User-agent: ClaudeBot\n", "Disallow: /\n\nUser-agent: *")
	wants(t, "sitemap", do(t, p, "GET", liveHost, "/sitemap.xml", nil).body, "<loc>https://al-noor.renoxium.test/products/1</loc>")
	home := do(t, p, "GET", liveHost, "/", nil)
	wants(t, "home", home.body, `<meta name="description" content="Perfumes from Deira.">`,
		`<meta property="og:image" content="https://al-noor.renoxium.test/_rx/files/share.png">`,
		`<meta name="twitter:card" content="summary_large_image">`,
		`<meta name="google-site-verification" content="abc-123">`)
	if r := do(t, p, "GET", liveHost, "/products/1", nil); strings.Contains(r.body, "google-site-verification") {
		t.Error("the verification code is on every page")
	}
	wants(t, "llms.txt", do(t, p, "GET", liveHost, "/llms.txt", nil).body, "> Perfumes from Deira.", "[Royal Oud](https://al-noor.renoxium.test/products/1)")

	// A definition that writes to the database from the sitemap is refused by the read-only query.
	def.SEO.Sitemap = []string{"SELECT '/x' AS path; DELETE FROM products"}
	if res, _ := p.ApplyChange(ctx, "al-noor", def); res.OK {
		if r := do(t, p, "GET", previewHost, "/", nil); !strings.Contains(r.body, "Royal Oud") {
			t.Fatal("a sitemap query changed the database")
		}
	}

	// The app's own robots.txt wins.
	def.SEO = nil
	def.Templates["robots"] = "User-agent: *\nDisallow: /secret\n"
	def.Routes = append(def.Routes, appdef.Route{Method: "GET", Path: "/robots.txt", Template: "robots"})
	if res, err := p.ApplyChange(ctx, "al-noor", def); err != nil || !res.OK {
		t.Fatal(err, res)
	}
	if r := do(t, p, "GET", previewHost, "/robots.txt", nil); !strings.Contains(r.body, "Disallow: /secret") {
		t.Errorf("own robots.txt: %q", r.body)
	}
}

func TestSEODefinitionIsChecked(t *testing.T) {
	def := catalog(t)
	def.SEO = &appdef.SEO{Image: "http://x/y.png", Languages: []string{"English"}, Sitemap: []string{"about"},
		AICrawlers: "maybe", Verify: map[string]string{"google": `<meta name="x">`, "altavista": "1"}}
	problems := strings.Join(def.Validate(), "\n")
	for _, want := range []string{"seo.image", "seo.languages", "seo.sitemap 0", "seo.ai_crawlers", "seo.verify.google", `"altavista"`} {
		if !strings.Contains(problems, want) {
			t.Errorf("no problem reported for %s:\n%s", want, problems)
		}
	}
}
