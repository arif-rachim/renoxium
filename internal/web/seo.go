package web

import (
	"bytes"
	"html"
	"net/http"
	"regexp"
	"runtime/debug"
	"strings"

	"github.com/arif-rachim/renoxium/internal/seo"
)

// publicPages are the site's pages meant for search engines: the only ones
// in the sitemap, and the ones that get a canonical address.
var publicPages = []string{"/", "/ar", "/templates", "/connect", "/support", "/privacy", "/terms"}

func isPublicPage(path string) bool {
	for _, p := range publicPages {
		if p == path {
			return true
		}
	}
	return false
}

func (s *Site) registerSEO() {
	s.mux.HandleFunc("GET /robots.txt", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Cache-Control", "public, max-age=3600")
		// Everything else needs an account, so crawlers only see sign-in forms there.
		w.Write([]byte("User-agent: *\nDisallow: /apps/\nDisallow: /oauth/\nDisallow: /login\nDisallow: /signup\n\nSitemap: " + s.base + "/sitemap.xml\n"))
	})
	s.mux.HandleFunc("GET /sitemap.xml", func(w http.ResponseWriter, r *http.Request) {
		var b strings.Builder
		b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n" + `<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">` + "\n")
		lastmod := ""
		if d := buildDate(); d != "" {
			lastmod = "<lastmod>" + d + "</lastmod>"
		}
		for _, p := range publicPages {
			b.WriteString("<url><loc>" + html.EscapeString(s.base+p) + "</loc>" + lastmod + "</url>\n")
		}
		b.WriteString("</urlset>\n")
		w.Header().Set("Content-Type", "application/xml; charset=utf-8")
		w.Header().Set("Cache-Control", "public, max-age=3600")
		w.Write([]byte(b.String()))
	})
	s.mux.HandleFunc("GET /llms-full.txt", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Write([]byte(s.llmsFull()))
	})
}

// llmsFull is everything an AI assistant needs to describe Renoxium to
// someone: what it is, the templates, the plans, and how to connect.
func (s *Site) llmsFull() string {
	var b strings.Builder
	b.WriteString("# Renoxium\n\n> " + siteDescription + "\n\n")
	b.WriteString("Owners describe their business to ChatGPT or Claude; the assistant builds the website on Renoxium, tests it on a private preview, and the owner publishes it. Every site is rendered on the server, works in English and Arabic, takes card payments through the owner's Stripe account or cash on delivery, and has an admin panel for orders, bookings and products. Each site can also serve its own tools to customers' AI assistants.\n\n")
	b.WriteString("## Templates\n\nFinished websites that can be tried live at " + s.base + "/templates:\n\n")
	for _, c := range s.galleryCards() {
		b.WriteString("- " + c.Title + " (" + c.Name + "): " + c.Summary + " For " + c.Suits + ". Demo: " + c.DemoURL + "/\n")
	}
	b.WriteString("\n## Plans\n\n")
	for _, p := range sitePlans {
		b.WriteString("- " + p.Name + ": AED " + p.Price + " a month. " + p.Text + "\n")
	}
	b.WriteString("\nStripe charges its own card fees on every plan. Renoxium has no credits: building and changing a site through the assistant is unlimited on every plan and never charged per request. The owner uses their own ChatGPT or Claude account; Claude's free plan is enough (it allows one custom connector), and in ChatGPT adding the connector by its address needs developer mode, which is on its paid plans.\n\n")
	b.WriteString("## Pages\n\n")
	for _, p := range publicPages {
		b.WriteString("- " + s.base + p + "\n")
	}
	return b.String() + "\n" + s.connectBody()
}

const siteDescription = "Renoxium builds and hosts websites for UAE small businesses from a chat with ChatGPT or Claude: online stores, bookings, rentals, quotes and listings, with card payments, cash on delivery, English and Arabic."

// sitePlans mirrors the pricing section of the landing page.
var sitePlans = []struct{ Name, Price, Text string }{
	{"Free", "0", "Unlimited building and changes; 1 site; Renoxium takes 2% of card payments; 50 WhatsApp sign-in codes a month; 200 MB of images; a small \"Made with Renoxium\" badge."},
	{"Pro", "99", "Unlimited building and changes; 3 sites; no Renoxium fee on card payments; 500 WhatsApp sign-in codes a month; 2 GB of images; your own domain; 2 staff per site; no badge."},
	{"Business", "299", "Unlimited building and changes; 10 sites; no Renoxium fee on card payments; 2,000 WhatsApp sign-in codes a month; 10 GB of images; your own domain; 10 staff per site; no badge."},
}

var (
	faqRE     = regexp.MustCompile(`(?s)<details[^>]*><summary>(.*?)</summary>(.*?)</details>`)
	stripTags = regexp.MustCompile(`<[^>]*>`)
)

// siteJSONLD is the home page's structured data: the organization, the
// product with its plans, and the questions answered on the page itself.
func (s *Site) siteJSONLD(page []byte) any {
	offers := []any{}
	for _, p := range sitePlans {
		offers = append(offers, map[string]any{"@type": "Offer", "name": p.Name, "price": p.Price, "priceCurrency": "AED", "description": p.Text})
	}
	graph := []any{
		s.organization(),
		map[string]any{"@type": "WebSite", "@id": s.base + "/#site", "name": "Renoxium", "url": s.base + "/", "inLanguage": []string{"en", "ar"}, "publisher": map[string]any{"@id": s.base + "/#org"}},
		map[string]any{"@type": "SoftwareApplication", "name": "Renoxium", "url": s.base + "/", "applicationCategory": "BusinessApplication",
			"operatingSystem": "Web", "description": siteDescription, "offers": offers, "publisher": map[string]any{"@id": s.base + "/#org"}},
	}
	text := func(b []byte) string {
		return strings.Join(strings.Fields(html.UnescapeString(string(stripTags.ReplaceAll(b, []byte(" "))))), " ")
	}
	var questions []any
	for _, m := range faqRE.FindAllSubmatch(page, -1) {
		if q, a := text(m[1]), text(m[2]); q != "" && a != "" {
			questions = append(questions, map[string]any{"@type": "Question", "name": q,
				"acceptedAnswer": map[string]any{"@type": "Answer", "text": a}})
		}
	}
	if len(questions) > 0 {
		graph = append(graph, map[string]any{"@type": "FAQPage", "mainEntity": questions})
	}
	return map[string]any{"@context": "https://schema.org", "@graph": graph}
}

// organization is the company as structured data, with the contact details
// and profile pages that are configured.
func (s *Site) organization() map[string]any {
	l := s.legalOrDefaults()
	org := map[string]any{"@type": "Organization", "@id": s.base + "/#org", "name": "Renoxium", "url": s.base + "/", "logo": s.base + "/og-image.png"}
	if l.Company != "" && l.Company != "Renoxium" {
		org["legalName"] = l.Company
	}
	if l.Email != "" {
		org["email"] = l.Email
	}
	if l.Address != "" {
		org["address"] = l.Address
	}
	contact := map[string]any{"@type": "ContactPoint", "contactType": "customer support", "availableLanguage": []string{"en", "ar"}}
	if l.Email != "" {
		contact["email"] = l.Email
	}
	if wa := l.WA(); wa != "" {
		contact["telephone"] = "+" + wa
	}
	if len(contact) > 3 {
		org["contactPoint"] = contact
	}
	var same []string
	for _, x := range l.Socials() {
		same = append(same, x.URL)
	}
	if len(same) > 0 {
		org["sameAs"] = same
	}
	return org
}

// buildDate is the day this binary's source was committed (YYYY-MM-DD), the
// sitemap's lastmod; empty when the build carries no version control stamp.
func buildDate() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}
	for _, kv := range info.Settings {
		if kv.Key == "vcs.time" && len(kv.Value) >= 10 {
			return kv.Value[:10]
		}
	}
	return ""
}

// pageBuffer holds a public page until its <head> is completed.
type pageBuffer struct {
	http.ResponseWriter
	status int
	body   bytes.Buffer
}

func (b *pageBuffer) WriteHeader(status int) { b.status = status }
func (b *pageBuffer) Write(p []byte) (int, error) {
	if b.status == 0 {
		b.status = http.StatusOK
	}
	return b.body.Write(p)
}

// servePublic serves one of publicPages with its canonical address and,
// on the home page, structured data.
func (s *Site) servePublic(w http.ResponseWriter, r *http.Request) {
	buf := &pageBuffer{ResponseWriter: w}
	s.mux.ServeHTTP(buf, r)
	page := buf.body.Bytes()
	if buf.status == 0 {
		buf.status = http.StatusOK
	}
	if buf.status == http.StatusOK && strings.HasPrefix(w.Header().Get("Content-Type"), "text/html") {
		h := seo.Head{Canonical: s.base + r.URL.Path, SiteName: "Renoxium"}
		// Signed-in visitors get their dashboard at /, which has no landing copy.
		if (r.URL.Path == "/" || r.URL.Path == "/ar") && bytes.Contains(page, []byte(`property="og:title"`)) {
			h.JSONLD = s.siteJSONLD(page)
		}
		page = seo.Inject(page, h)
	}
	w.WriteHeader(buf.status)
	w.Write(page)
}
