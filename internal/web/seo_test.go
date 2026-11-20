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
	for _, want := range []string{"# Renoxium", "## Templates", "Online store (store-uae)", "- Pro: AED 99 a month", e.base + "/mcp", "## Claude"} {
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
