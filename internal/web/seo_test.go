package web_test

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
	"testing"
)

// The site tells search engines and AI assistants where its public pages
// are, and its home page describes itself in structured data that matches
// what the page says.
func TestSiteIsFound(t *testing.T) {
	e := newEnv(t)
	get := func(path string) (int, string) {
		t.Helper()
		r, err := http.Get(e.base + path)
		if err != nil {
			t.Fatal(err)
		}
		return r.StatusCode, read(t, r)
	}
	if status, body := get("/robots.txt"); status != 200 || !strings.Contains(body, "Sitemap: "+e.base+"/sitemap.xml") || !strings.Contains(body, "Disallow: /apps/") {
		t.Errorf("robots.txt: %d %s", status, body)
	}
	_, sitemap := get("/sitemap.xml")
	for _, page := range []string{"/", "/templates", "/connect", "/support", "/privacy", "/terms"} {
		if !strings.Contains(sitemap, "<loc>"+e.base+page+"</loc>") {
			t.Errorf("sitemap lacks %s", page)
		}
		status, body := get(page)
		if status != 200 || !strings.Contains(body, `<link rel="canonical" href="`+e.base+page+`">`) {
			t.Errorf("%s: %d, canonical address missing", page, status)
		}
		if strings.Count(body, "</head>") != 1 || strings.Count(body, `rel="canonical"`) != 1 {
			t.Errorf("%s: head changed twice", page)
		}
	}

	_, home := get("/")
	m := regexp.MustCompile(`(?s)<script type="application/ld\+json">(.*?)</script>`).FindStringSubmatch(home)
	if m == nil {
		t.Fatal("the home page has no structured data")
	}
	var ld struct {
		Graph []map[string]any `json:"@graph"`
	}
	if err := json.Unmarshal([]byte(m[1]), &ld); err != nil {
		t.Fatal(err)
	}
	types := map[string]map[string]any{}
	for _, node := range ld.Graph {
		types[node["@type"].(string)] = node
	}
	for _, want := range []string{"Organization", "WebSite", "SoftwareApplication", "FAQPage"} {
		if types[want] == nil {
			t.Errorf("structured data lacks %s", want)
		}
	}
	// The plans in the data are the ones on the page.
	for _, offer := range types["SoftwareApplication"]["offers"].([]any) {
		if price := offer.(map[string]any)["price"].(string); price != "0" && !strings.Contains(home, "AED "+price) {
			t.Errorf("plan at AED %s is not on the page", price)
		}
	}
	questions := types["FAQPage"]["mainEntity"].([]any)
	if q := questions[0].(map[string]any); len(questions) < 3 || q["name"] != "Do I need to know anything technical?" {
		t.Errorf("questions: %v", questions)
	}

	status, full := get("/llms-full.txt")
	for _, want := range []string{"# Renoxium", "## Templates", "Online store (store-uae)", "- Professional: AED 0 a month", "- Business: a price agreed with Renoxium", e.base + "/mcp", "## Claude"} {
		if status != 200 || !strings.Contains(full, want) {
			t.Errorf("llms-full.txt (%d) lacks %q", status, want)
		}
	}
	// Unknown pages are still 404, and private ones get no canonical address.
	if status, _ := get("/nothing-here"); status != 404 {
		t.Errorf("unknown page: %d", status)
	}
	if _, body := get("/login"); strings.Contains(body, `rel="canonical"`) {
		t.Error("the sign-in page has a canonical address")
	}
}

// The home page in each language is its own indexed page: its own address,
// links to the other language, one heading in one language, the same
// message in the title and the link preview, and a short description.
func TestHomePagesInBothLanguages(t *testing.T) {
	e := newEnv(t)
	e.site.Legal.Address = "Office 1, Sample Park, Sharjah, UAE"
	e.site.Legal.Email = "support@renoxium.test"
	e.site.Legal.WhatsApp = "+971 50 000 0000"
	e.site.Legal.Social = []string{"https://www.linkedin.com/company/sample", " https://www.instagram.com/sample", "http://not-https.example/x", "nonsense"}
	get := func(path string) string {
		t.Helper()
		r, err := http.Get(e.base + path)
		if err != nil {
			t.Fatal(err)
		}
		return read(t, r)
	}
	attr := func(page, pattern string) string {
		m := regexp.MustCompile(pattern).FindStringSubmatch(page)
		if m == nil {
			return ""
		}
		return m[1]
	}
	arabic := regexp.MustCompile(`\p{Arabic}`)
	for _, c := range []struct {
		path, lang string
		arabicH1   bool
	}{{"/", `<html lang="en">`, false}, {"/ar", `<html lang="ar" dir="rtl">`, true}} {
		page := get(c.path)
		for _, want := range []string{c.lang, `<link rel="canonical" href="` + e.base + c.path + `">`,
			`hreflang="en" href="` + e.base + `/"`, `hreflang="ar" href="` + e.base + `/ar"`, `hreflang="x-default" href="` + e.base + `/"`,
			`name="twitter:title"`, `name="twitter:description"`, `name="twitter:image" content="` + e.base + `/og-image.png"`,
			`<a href="mailto:support@renoxium.test">`, `href="https://wa.me/971500000000"`, "Office 1, Sample Park, Sharjah, UAE",
			`<a href="https://www.linkedin.com/company/sample" rel="me noopener">LinkedIn</a>`, `rel="me noopener">Instagram</a>`,
			`application/ld+json`} {
			if !strings.Contains(page, want) {
				t.Errorf("%s lacks %q", c.path, want)
			}
		}
		if strings.Contains(page, "not-https.example") || strings.Contains(page, "nonsense") {
			t.Errorf("%s links a profile address that is not https", c.path)
		}
		title := attr(page, `<title>([^<]*)</title>`)
		if og := attr(page, `property="og:title" content="([^"]*)"`); title == "" || og != title || attr(page, `name="twitter:title" content="([^"]*)"`) != title {
			t.Errorf("%s: title %q, og:title %q", c.path, title, og)
		}
		if d := attr(page, `name="description" content="([^"]*)"`); len([]rune(d)) < 80 || len([]rune(d)) > 160 {
			t.Errorf("%s: description of %d characters", c.path, len([]rune(d)))
		}
		h1 := regexp.MustCompile(`(?s)<h1[^>]*>(.*?)</h1>`).FindAllStringSubmatch(page, -1)
		if len(h1) != 1 || arabic.MatchString(h1[0][1]) != c.arabicH1 || (c.arabicH1 && strings.Contains(h1[0][1], "Orders")) {
			t.Errorf("%s: headings %v", c.path, h1)
		}
	}

	_, sitemap := func() (int, string) { r, _ := http.Get(e.base + "/sitemap.xml"); return r.StatusCode, read(t, r) }()
	if !strings.Contains(sitemap, "<loc>"+e.base+"/ar</loc>") {
		t.Error("the sitemap lacks /ar")
	}

	// The organization carries the configured contact details and profiles.
	m := regexp.MustCompile(`(?s)<script type="application/ld\+json">(.*?)</script>`).FindStringSubmatch(get("/"))
	var ld struct {
		Graph []map[string]any `json:"@graph"`
	}
	if m == nil || json.Unmarshal([]byte(m[1]), &ld) != nil || len(ld.Graph) == 0 {
		t.Fatal("no structured data")
	}
	org := ld.Graph[0]
	same, _ := org["sameAs"].([]any)
	contact, _ := org["contactPoint"].(map[string]any)
	if org["@type"] != "Organization" || org["email"] != "support@renoxium.test" || org["address"] != "Office 1, Sample Park, Sharjah, UAE" ||
		len(same) != 2 || same[0] != "https://www.linkedin.com/company/sample" || contact["telephone"] != "+971500000000" {
		t.Errorf("organization: %v", org)
	}

	// Without configuration nothing is invented: no profiles, no phone.
	plain := newEnv(t)
	r, _ := http.Get(plain.base + "/")
	if page := read(t, r); strings.Contains(page, "wa.me/") || strings.Contains(page, `"sameAs"`) || strings.Contains(page, "linkedin.com") {
		t.Error("an unconfigured site shows a WhatsApp number or profile links")
	}
}
